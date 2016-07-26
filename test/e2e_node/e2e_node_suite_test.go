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

// To run tests in this suite
// NOTE: This test suite requires password-less sudo capabilities to run the kubelet and kube-apiserver.
package e2e_node

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"
	"time"

	commontest "k8s.io/kubernetes/test/e2e/common"
	"k8s.io/kubernetes/test/e2e/framework"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	more_reporters "github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"
)

var e2es *e2eService

var prePullImages = flag.Bool("prepull-images", true, "If true, prepull images so image pull failures do not cause test failures.")
var junitFileNumber = flag.Int("junit-file-number", 1, "Used to create junit filename - e.g. junit_01.xml.")

func init() {
	framework.RegisterCommonFlags()
	framework.RegisterNodeFlags()
}

func TestE2eNode(t *testing.T) {
	flag.Parse()

	rand.Seed(time.Now().UTC().UnixNano())
	RegisterFailHandler(Fail)
	reporters := []Reporter{}
	if *reportDir != "" {
		// Create the directory if it doesn't already exists
		if err := os.MkdirAll(*reportDir, 0755); err != nil {
			glog.Errorf("Failed creating report directory: %v", err)
		} else {
			// Configure a junit reporter to write to the directory
			junitFile := fmt.Sprintf("junit_%02d.xml", *junitFileNumber)
			junitPath := path.Join(*reportDir, junitFile)
			reporters = append(reporters, more_reporters.NewJUnitReporter(junitPath))
		}
	}
	RunSpecsWithDefaultAndCustomReporters(t, "E2eNode Suite", reporters)
}

// Setup the kubelet on the node
var _ = BeforeSuite(func() {
	if *buildServices {
		buildGo()
	}
	if framework.TestContext.NodeName == "" {
		output, err := exec.Command("hostname").CombinedOutput()
		if err != nil {
			glog.Fatalf("Could not get node name from hostname %v.  Output:\n%s", err, output)
		}
		framework.TestContext.NodeName = strings.TrimSpace(fmt.Sprintf("%s", output))
	}

	// Pre-pull the images tests depend on so we can fail immediately if there is an image pull issue
	// This helps with debugging test flakes since it is hard to tell when a test failure is due to image pulling.
	if *prePullImages {
		glog.Infof("Pre-pulling images so that they are cached for the tests.")
		err := PrePullAllImages()
		Expect(err).ShouldNot(HaveOccurred())
	}

	// TODO(yifan): Temporary workaround to disable coreos from auto restart
	// by masking the locksmithd.
	// We should mask locksmithd when provisioning the machine.
	maskLocksmithdOnCoreos()

	if *startServices {
		e2es = newE2eService(framework.TestContext.NodeName, framework.TestContext.CgroupsPerQOS)
		if err := e2es.start(); err != nil {
			Fail(fmt.Sprintf("Unable to start node services.\n%v", err))
		}
		glog.Infof("Node services started.  Running tests...")
	} else {
		glog.Infof("Running tests without starting services.")
	}

	// Reference common test to make the import valid.
	commontest.CurrentSuite = commontest.NodeE2E
})

// Tear down the kubelet on the node
var _ = AfterSuite(func() {
	if e2es != nil {
		e2es.getLogFiles()
		if *startServices && *stopServices {
			glog.Infof("Stopping node services...")
			e2es.stop()
		}
	}

	glog.Infof("Tests Finished")
})

func maskLocksmithdOnCoreos() {
	data, err := ioutil.ReadFile("/etc/os-release")
	if err != nil {
		glog.Fatalf("Could not read /etc/os-release: %v", err)
	}
	if bytes.Contains(data, []byte("ID=coreos")) {
		if output, err := exec.Command("sudo", "systemctl", "mask", "--now", "locksmithd").CombinedOutput(); err != nil {
			glog.Fatalf("Could not mask locksmithd: %v, output: %q", err, string(output))
		}
		glog.Infof("Locksmithd is masked successfully")
	}
}
