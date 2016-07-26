/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// To run the e2e tests against one or more hosts on gce:
// $ go run run_e2e.go --logtostderr --v 2 --ssh-env gce --hosts <comma separated hosts>
// To run the e2e tests against one or more images on gce and provision them:
// $ go run run_e2e.go --logtostderr --v 2 --project <project> --zone <zone> --ssh-env gce --images <comma separated images>
package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"k8s.io/kubernetes/test/e2e_node"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"github.com/pborman/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/compute/v1"
)

var testArgs = flag.String("test_args", "", "Space-separated list of arguments to pass to Ginkgo test runner.")
var instanceNamePrefix = flag.String("instance-name-prefix", "", "prefix for instance names")
var zone = flag.String("zone", "", "gce zone the hosts live in")
var project = flag.String("project", "", "gce project the hosts live in")
var imageConfigFile = flag.String("image-config-file", "", "yaml file describing images to run")
var imageProject = flag.String("image-project", "", "gce project the hosts live in")
var images = flag.String("images", "", "images to test")
var hosts = flag.String("hosts", "", "hosts to test")
var cleanup = flag.Bool("cleanup", true, "If true remove files from remote hosts and delete temporary instances")
var deleteInstances = flag.Bool("delete-instances", true, "If true, delete any instances created")
var buildOnly = flag.Bool("build-only", false, "If true, build e2e_node_test.tar.gz and exit.")
var setupNode = flag.Bool("setup-node", false, "When true, current user will be added to docker group on the test machine")
var instanceMetadata = flag.String("instance-metadata", "", "key/value metadata for instances separated by '=' or '<', 'k=v' means the key is 'k' and the value is 'v'; 'k<p' means the key is 'k' and the value is extracted from the local path 'p', e.g. k1=v1,k2<p2")

var computeService *compute.Service

type Archive struct {
	sync.Once
	path string
	err  error
}

var arc Archive

type TestResult struct {
	output string
	err    error
	host   string
	exitOk bool
}

// ImageConfig specifies what images should be run and how for these tests.
// It can be created via the `--images` and `--image-project` flags, or by
// specifying the `--image-config-file` flag, pointing to a json or yaml file
// of the form:
//
//     images:
//       short-name:
//         image: gce-image-name
//         project: gce-image-project
type ImageConfig struct {
	Images map[string]GCEImage `json:"images"`
}

type GCEImage struct {
	Image   string `json:"image"`
	Project string `json:"project"`
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UTC().UnixNano())
	if *buildOnly {
		// Build the archive and exit
		e2e_node.CreateTestArchive()
		return
	}

	if *hosts == "" && *imageConfigFile == "" && *images == "" {
		glog.Fatalf("Must specify one of --image-config-file, --hosts, --images.")
	}
	gceImages := &ImageConfig{
		Images: make(map[string]GCEImage),
	}
	if *imageConfigFile != "" {
		// parse images
		imageConfigData, err := ioutil.ReadFile(*imageConfigFile)
		if err != nil {
			glog.Fatalf("Could not read image config file provided: %v", err)
		}
		err = yaml.Unmarshal(imageConfigData, gceImages)
		if err != nil {
			glog.Fatalf("Could not parse image config file: %v", err)
		}
	}

	// Allow users to specify additional images via cli flags for local testing
	// convenience; merge in with config file
	if *images != "" {
		if *imageProject == "" {
			glog.Fatal("Must specify --image-project if you specify --images")
		}
		cliImages := strings.Split(*images, ",")
		for _, img := range cliImages {
			gceImages.Images[img] = GCEImage{
				Image:   img,
				Project: *imageProject,
			}
		}
	}

	if len(gceImages.Images) != 0 && *zone == "" {
		glog.Fatal("Must specify --zone flag")
	}
	for shortName, image := range gceImages.Images {
		if image.Project == "" {
			glog.Fatalf("Invalid config for %v; must specify a project", shortName)
		}
	}
	if len(gceImages.Images) != 0 {
		if *project == "" {
			glog.Fatal("Must specify --project flag to launch images into")
		}
	}
	if *instanceNamePrefix == "" {
		*instanceNamePrefix = "tmp-node-e2e-" + uuid.NewUUID().String()[:8]
	}

	// Setup coloring
	stat, _ := os.Stdout.Stat()
	useColor := (stat.Mode() & os.ModeCharDevice) != 0
	blue := ""
	noColour := ""
	if useColor {
		blue = "\033[0;34m"
		noColour = "\033[0m"
	}

	go arc.getArchive()
	defer arc.deleteArchive()

	var err error
	computeService, err = getComputeClient()
	if err != nil {
		glog.Fatalf("Unable to create gcloud compute service using defaults.  Make sure you are authenticated. %v", err)
	}

	results := make(chan *TestResult)
	running := 0
	for shortName, image := range gceImages.Images {
		running++
		fmt.Printf("Initializing e2e tests using image %s.\n", shortName)
		go func(image, imageProject string, junitFileNum int) {
			results <- testImage(image, imageProject, junitFileNum)
		}(image.Image, image.Project, running)
	}
	if *hosts != "" {
		for _, host := range strings.Split(*hosts, ",") {
			fmt.Printf("Initializing e2e tests using host %s.\n", host)
			running++
			go func(host string, junitFileNum int) {
				results <- testHost(host, *cleanup, junitFileNum, *setupNode)
			}(host, running)
		}
	}

	// Wait for all tests to complete and emit the results
	errCount := 0
	exitOk := true
	for i := 0; i < running; i++ {
		tr := <-results
		host := tr.host
		fmt.Printf("%s================================================================%s\n", blue, noColour)
		if tr.err != nil {
			errCount++
			fmt.Printf("Failure Finished Host %s Test Suite\n%s\n%v\n", host, tr.output, tr.err)
		} else {
			fmt.Printf("Success Finished Host %s Test Suite\n%s\n", host, tr.output)
		}
		exitOk = exitOk && tr.exitOk
		fmt.Printf("%s================================================================%s\n", blue, noColour)
	}

	// Set the exit code if there were failures
	if !exitOk {
		fmt.Printf("Failure: %d errors encountered.", errCount)
		os.Exit(1)
	}
}

func (a *Archive) getArchive() (string, error) {
	a.Do(func() { a.path, a.err = e2e_node.CreateTestArchive() })
	return a.path, a.err
}

func (a *Archive) deleteArchive() {
	path, err := a.getArchive()
	if err != nil {
		return
	}
	os.Remove(path)
}

// Run tests in archive against host
func testHost(host string, deleteFiles bool, junitFileNum int, setupNode bool) *TestResult {
	instance, err := computeService.Instances.Get(*project, *zone, host).Do()
	if err != nil {
		return &TestResult{
			err:    err,
			host:   host,
			exitOk: false,
		}
	}
	if strings.ToUpper(instance.Status) != "RUNNING" {
		err = fmt.Errorf("instance %s not in state RUNNING, was %s.", host, instance.Status)
		return &TestResult{
			err:    err,
			host:   host,
			exitOk: false,
		}
	}
	externalIp := getExternalIp(instance)
	if len(externalIp) > 0 {
		e2e_node.AddHostnameIp(host, externalIp)
	}

	path, err := arc.getArchive()
	if err != nil {
		// Don't log fatal because we need to do any needed cleanup contained in "defer" statements
		return &TestResult{
			err: fmt.Errorf("unable to create test archive %v.", err),
		}
	}

	output, exitOk, err := e2e_node.RunRemote(path, host, deleteFiles, junitFileNum, setupNode, *testArgs)
	return &TestResult{
		output: output,
		err:    err,
		host:   host,
		exitOk: exitOk,
	}
}

// Provision a gce instance using image and run the tests in archive against the instance.
// Delete the instance afterward.
func testImage(image, imageProject string, junitFileNum int) *TestResult {
	host, err := createInstance(image, imageProject)
	if *deleteInstances {
		defer deleteInstance(image)
	}
	if err != nil {
		return &TestResult{
			err: fmt.Errorf("unable to create gce instance with running docker daemon for image %s.  %v", image, err),
		}
	}

	// Only delete the files if we are keeping the instance and want it cleaned up.
	// If we are going to delete the instance, don't bother with cleaning up the files
	deleteFiles := !*deleteInstances && *cleanup
	return testHost(host, deleteFiles, junitFileNum, *setupNode)
}

// Provision a gce instance using image
func createInstance(image, imageProject string) (string, error) {
	name := imageToInstanceName(image)
	i := &compute.Instance{
		Name:        name,
		MachineType: machineType(),
		NetworkInterfaces: []*compute.NetworkInterface{
			{
				AccessConfigs: []*compute.AccessConfig{
					{
						Type: "ONE_TO_ONE_NAT",
						Name: "External NAT",
					},
				}},
		},
		Disks: []*compute.AttachedDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Type:       "PERSISTENT",
				InitializeParams: &compute.AttachedDiskInitializeParams{
					SourceImage: sourceImage(image, imageProject),
				},
			},
		},
	}
	if *instanceMetadata != "" {
		raw := parseInstanceMetadata(*instanceMetadata)
		i.Metadata = &compute.Metadata{}
		metadata := []*compute.MetadataItems{}
		for k, v := range raw {
			val := v
			metadata = append(metadata, &compute.MetadataItems{
				Key:   k,
				Value: &val,
			})
		}
		i.Metadata.Items = metadata
	}
	op, err := computeService.Instances.Insert(*project, *zone, i).Do()
	if err != nil {
		return "", err
	}
	if op.Error != nil {
		return "", fmt.Errorf("could not create instance %s: %+v", name, op.Error)
	}

	instanceRunning := false
	for i := 0; i < 30 && !instanceRunning; i++ {
		if i > 0 {
			time.Sleep(time.Second * 20)
		}
		var instance *compute.Instance
		instance, err = computeService.Instances.Get(*project, *zone, name).Do()
		if err != nil {
			continue
		}
		if strings.ToUpper(instance.Status) != "RUNNING" {
			err = fmt.Errorf("instance %s not in state RUNNING, was %s.", name, instance.Status)
			continue
		}
		externalIp := getExternalIp(instance)
		if len(externalIp) > 0 {
			e2e_node.AddHostnameIp(name, externalIp)
		}
		var output string
		output, err = e2e_node.RunSshCommand("ssh", e2e_node.GetHostnameOrIp(name), "--", "sudo", "docker", "version")
		if err != nil {
			err = fmt.Errorf("instance %s not running docker daemon - Command failed: %s", name, output)
			continue
		}
		if !strings.Contains(output, "Server") {
			err = fmt.Errorf("instance %s not running docker daemon - Server not found: %s", name, output)
			continue
		}
		instanceRunning = true
	}
	return name, err
}

func getExternalIp(instance *compute.Instance) string {
	for i := range instance.NetworkInterfaces {
		ni := instance.NetworkInterfaces[i]
		for j := range ni.AccessConfigs {
			ac := ni.AccessConfigs[j]
			if len(ac.NatIP) > 0 {
				return ac.NatIP
			}
		}
	}
	return ""
}

func getComputeClient() (*compute.Service, error) {
	const retries = 10
	const backoff = time.Second * 6

	// Setup the gce client for provisioning instances
	// Getting credentials on gce jenkins is flaky, so try a couple times
	var err error
	var cs *compute.Service
	for i := 0; i < retries; i++ {
		if i > 0 {
			time.Sleep(backoff)
		}

		var client *http.Client
		client, err = google.DefaultClient(oauth2.NoContext, compute.ComputeScope)
		if err != nil {
			continue
		}

		cs, err = compute.New(client)
		if err != nil {
			continue
		}
		return cs, nil
	}
	return nil, err
}

func deleteInstance(image string) {
	_, err := computeService.Instances.Delete(*project, *zone, imageToInstanceName(image)).Do()
	if err != nil {
		glog.Infof("Error deleting instance %s", imageToInstanceName(image))
	}
}

func parseInstanceMetadata(str string) map[string]string {
	metadata := make(map[string]string)
	ss := strings.Split(str, ",")
	for _, s := range ss {
		kv := strings.Split(s, "=")
		if len(kv) == 2 {
			metadata[kv[0]] = kv[1]
			continue
		}
		kp := strings.Split(s, "<")
		if len(kp) != 2 {
			glog.Errorf("Invalid instance metadata: %q", s)
			continue
		}
		v, err := ioutil.ReadFile(kp[1])
		if err != nil {
			glog.Errorf("Failed to read metadata file %q: %v", kp[1], err)
			continue
		}
		metadata[kp[0]] = string(v)
	}
	return metadata
}

func imageToInstanceName(image string) string {
	return *instanceNamePrefix + "-" + image
}

func sourceImage(image, imageProject string) string {
	return fmt.Sprintf("projects/%s/global/images/%s", imageProject, image)
}

func machineType() string {
	return fmt.Sprintf("zones/%s/machineTypes/n1-standard-1", *zone)
}
