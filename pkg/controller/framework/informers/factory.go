/*
Copyright 2015 The Kubernetes Authors.

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

package informers

import (
	"reflect"
	"sync"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/controller/framework"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/watch"
)

// SharedInformerFactory provides interface which holds unique informers for pods, nodes, namespaces, persistent volume
// claims and persistent volumes
type SharedInformerFactory interface {
	// Start starts informers that can start AFTER the API server and controllers have started
	Start(stopCh <-chan struct{})

	Pods() PodInformer
	Nodes() NodeInformer
	Namespaces() NamespaceInformer
	PersistentVolumeClaims() PVCInformer
	PersistentVolumes() PVInformer
}

type sharedInformerFactory struct {
	client        clientset.Interface
	lock          sync.Mutex
	defaultResync time.Duration
	informers     map[reflect.Type]framework.SharedIndexInformer
}

// NewSharedInformerFactory constructs a new instance of sharedInformerFactory
func NewSharedInformerFactory(client clientset.Interface, defaultResync time.Duration) SharedInformerFactory {
	return &sharedInformerFactory{
		client:        client,
		defaultResync: defaultResync,
		informers:     make(map[reflect.Type]framework.SharedIndexInformer),
	}
}

// Start initializes all requested informers.
func (s *sharedInformerFactory) Start(stopCh <-chan struct{}) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for _, informer := range s.informers {
		go informer.Run(stopCh)
	}
}

// Pods returns a SharedIndexInformer that lists and watches all pods
func (f *sharedInformerFactory) Pods() PodInformer {
	return &podInformer{sharedInformerFactory: f}
}

// Nodes returns a SharedIndexInformer that lists and watches all nodes
func (f *sharedInformerFactory) Nodes() NodeInformer {
	return &nodeInformer{sharedInformerFactory: f}
}

// Namespaces returns a SharedIndexInformer that lists and watches all namespaces
func (f *sharedInformerFactory) Namespaces() NamespaceInformer {
	return &namespaceInformer{sharedInformerFactory: f}
}

// PersistentVolumeClaims returns a SharedIndexInformer that lists and watches all persistent volume claims
func (f *sharedInformerFactory) PersistentVolumeClaims() PVCInformer {
	return &pvcInformer{sharedInformerFactory: f}
}

// PersistentVolumes returns a SharedIndexInformer that lists and watches all persistent volumes
func (f *sharedInformerFactory) PersistentVolumes() PVInformer {
	return &pvInformer{sharedInformerFactory: f}
}

// CreateSharedPodInformer returns a SharedIndexInformer that lists and watches all pods
func CreateSharedPodInformer(client clientset.Interface, resyncPeriod time.Duration) framework.SharedIndexInformer {
	sharedInformer := framework.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return client.Core().Pods(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.Core().Pods(api.NamespaceAll).Watch(options)
			},
		},
		&api.Pod{},
		resyncPeriod,
		cache.Indexers{},
	)

	return sharedInformer
}

// CreateSharedPodIndexInformer returns a SharedIndexInformer that lists and watches all pods
func CreateSharedPodIndexInformer(client clientset.Interface, resyncPeriod time.Duration) framework.SharedIndexInformer {
	sharedIndexInformer := framework.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return client.Core().Pods(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.Core().Pods(api.NamespaceAll).Watch(options)
			},
		},
		&api.Pod{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	return sharedIndexInformer
}

// CreateSharedNodeIndexInformer returns a SharedIndexInformer that lists and watches all nodes
func CreateSharedNodeIndexInformer(client clientset.Interface, resyncPeriod time.Duration) framework.SharedIndexInformer {
	sharedIndexInformer := framework.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return client.Core().Nodes().List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.Core().Nodes().Watch(options)
			},
		},
		&api.Node{},
		resyncPeriod,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return sharedIndexInformer
}

// CreateSharedPVCIndexInformer returns a SharedIndexInformer that lists and watches all PVCs
func CreateSharedPVCIndexInformer(client clientset.Interface, resyncPeriod time.Duration) framework.SharedIndexInformer {
	sharedIndexInformer := framework.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return client.Core().PersistentVolumeClaims(api.NamespaceAll).List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.Core().PersistentVolumeClaims(api.NamespaceAll).Watch(options)
			},
		},
		&api.PersistentVolumeClaim{},
		resyncPeriod,
		cache.Indexers{})

	return sharedIndexInformer
}

// CreateSharedPVIndexInformer returns a SharedIndexInformer that lists and watches all PVs
func CreateSharedPVIndexInformer(client clientset.Interface, resyncPeriod time.Duration) framework.SharedIndexInformer {
	sharedIndexInformer := framework.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return client.Core().PersistentVolumes().List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.Core().PersistentVolumes().Watch(options)
			},
		},
		&api.PersistentVolume{},
		resyncPeriod,
		cache.Indexers{})

	return sharedIndexInformer
}

// CreateSharedNamespaceIndexInformer returns a SharedIndexInformer that lists and watches namespaces
func CreateSharedNamespaceIndexInformer(client clientset.Interface, resyncPeriod time.Duration) framework.SharedIndexInformer {
	sharedIndexInformer := framework.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options api.ListOptions) (runtime.Object, error) {
				return client.Core().Namespaces().List(options)
			},
			WatchFunc: func(options api.ListOptions) (watch.Interface, error) {
				return client.Core().Namespaces().Watch(options)
			},
		},
		&api.Namespace{},
		resyncPeriod,
		cache.Indexers{})

	return sharedIndexInformer
}
