// Copyright 2019-2021 The Inspektor Gadget authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gadgets

import (
	"sync"

	gadgetv1alpha1 "github.com/kinvolk/inspektor-gadget/pkg/api/v1alpha1"
	pb "github.com/kinvolk/inspektor-gadget/pkg/gadgettracermanager/api"
	"github.com/kinvolk/inspektor-gadget/pkg/gadgettracermanager/pubsub"

	log "github.com/sirupsen/logrus"
	apimachineryruntime "k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TraceFactory interface {
	// Initialize gives the Resolver and the Client to the gadget. Gadgets
	// don't need to implement this method if they use BaseFactory as an
	// anonymous field.
	Initialize(Resolver Resolver, Client client.Client)

	// Delete request a gadget to release the information it has about a
	// trace. BaseFactory implements this method, so gadgets who embed
	// BaseFactory don't need to implement it.
	Delete(name string)

	// Operations gives the list of operations on a gadget that users can
	// call via the gadget.kinvolk.io/operation annotation.
	Operations() map[string]TraceOperation
}

type TraceFactoryWithScheme interface {
	TraceFactory

	// AddToScheme let gadgets inform the Trace controller of any scheme
	// they want to use
	AddToScheme(*apimachineryruntime.Scheme)
}

type TraceFactoryWithCapabilities interface {
	TraceFactory

	// OutputModesSupported returns the set of OutputMode supported by the
	// gadget. If the interface is not implemented, only "Status" is
	// supported.
	OutputModesSupported() map[string]struct{}
}

type TraceFactoryWithDocumentation interface {
	Description() string
}

type TraceFactoryWithDeleteTrace interface {
	TraceFactory

	// DeleteTrace is called by Delete. gadgets can promote DeleteTrace to
	// implement their own.
	DeleteTrace(name string, trace interface{})
}

// TraceOperation packages an operation on a gadget that users can call via the
// annotation gadget.kinvolk.io/operation.
type TraceOperation struct {
	// Operation is the function called by the controller
	Operation func(name string, trace *gadgetv1alpha1.Trace)

	// Doc documents the operation. It is used to generate the
	// documentation.
	Doc string

	// Order controls the ordering of the operation in the documentation.
	// It's only needed when ordering alphabetically is not suitable.
	Order int
}

type Resolver interface {
	// LookupMntnsByContainer returns the mount namespace inode of the container
	// specified in arguments or zero if not found
	LookupMntnsByContainer(namespace, pod, container string) uint64

	// LookupMntnsByPod returns the mount namespace inodes of all containers
	// belonging to the pod specified in arguments, indexed by the name of the
	// containers or an empty map if not found
	LookupMntnsByPod(namespace, pod string) map[string]uint64

	// LookupPIDByContainer returns the PID of the container
	// specified in arguments or zero if not found
	LookupPIDByContainer(namespace, pod, container string) uint32

	// LookupPIDByPod returns the PID of all containers belonging to
	// the pod specified in arguments, indexed by the name of the
	// containers or an empty map if not found
	LookupPIDByPod(namespace, pod string) map[string]uint32

	// GetContainersBySelector returns a slice of containers that match
	// the selector or an empty slice if there are not matches
	GetContainersBySelector(containerSelector *pb.ContainerSelector) []pb.ContainerDefinition

	// Subscribe returns the list of existing containers and registers a
	// callback for notifications about additions and deletions of
	// containers
	Subscribe(key interface{}, s pb.ContainerSelector, f pubsub.FuncNotify) []pb.ContainerDefinition

	// Unsubscribe undoes a previous call to Subscribe
	Unsubscribe(key interface{})

	PublishEvent(tracerID string, line string) error
}

type BaseFactory struct {
	Resolver Resolver
	Client   client.Client

	mu     sync.Mutex
	traces map[string]interface{}
}

func (f *BaseFactory) Initialize(r Resolver, c client.Client) {
	f.Resolver = r
	f.Client = c
}

func (f *BaseFactory) LookupOrCreate(name string, newTrace func() interface{}) interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.traces == nil {
		f.traces = make(map[string]interface{})
	} else {
		trace, ok := f.traces[name]
		if ok {
			return trace
		}
	}

	if newTrace == nil {
		return nil
	}

	trace := newTrace()
	f.traces[name] = trace

	return trace
}

func (f *BaseFactory) Delete(name string) {
	log.Infof("Deleting %s", name)
	f.mu.Lock()
	defer f.mu.Unlock()
	trace, ok := f.traces[name]
	if !ok {
		log.Infof("Deleting %s: does not exist", name)
		return
	}
	factory, ok := TraceFactory(f).(TraceFactoryWithDeleteTrace)
	if ok {
		factory.DeleteTrace(name, trace)
	}
	delete(f.traces, name)
	return
}

func (f *BaseFactory) Operations() map[string]TraceOperation {
	return map[string]TraceOperation{}
}