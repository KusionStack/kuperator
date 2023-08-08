/*
Copyright 2023 The KusionStack Authors.

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

package expectations

import (
	"fmt"
	"sync/atomic"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"kusionstack.io/kafed/pkg/log"
)

const (
	// If a watch drops a delete event for a pod, it'll take this long
	// before a dormant controller waiting for those packets is woken up anyway. It is
	// specifically targeted at the case where some problem prevents an update
	// of expectations, without it the controller could stay asleep forever. This should
	// be set based on the expected latency of watch events.
	//
	// Currently a controller can service (create *and* observe the watch events for said
	// creation) about 10 pods a second, so it takes about 1 min to service
	// 500 pods. Just creation is limited to 20qps, and watching happens with ~10-30s
	// latency/pod at the scale of 3000 pods over 100 nodes.
	ExpectationsTimeout = 10 * time.Minute
)

type ControllerExpectationsInterface interface {
	GetExpectations(controllerKey string) (*ControlleeExpectations, bool, error)
	SatisfiedExpectations(controllerKey string) bool
	DeleteExpectations(controllerKey string)
	SetExpectations(controllerKey string, add, del int) error
	ExpectCreations(controllerKey string, adds int) error
	ExpectDeletions(controllerKey string, dels int) error
	CreationObserved(controllerKey string)
	DeletionObserved(controllerKey string)
	RaiseExpectations(controllerKey string, add, del int)
	LowerExpectations(controllerKey string, add, del int)
}

// ControllerExpectations is a cache mapping controllers to what they expect to see before being woken up for a sync.
type ControllerExpectations struct {
	cache.Store
	name   string
	logger *log.Logger
}

// GetExpectations returns the ControlleeExpectations of the given controller.
func (r *ControllerExpectations) GetExpectations(controllerKey string) (*ControlleeExpectations, bool, error) {
	if exp, exists, err := r.GetByKey(controllerKey); err == nil && exists {
		return exp.(*ControlleeExpectations), true, nil
	} else {
		return nil, false, err
	}
}

// DeleteExpectations deletes the expectations of the given controller from the TTLStore.
func (r *ControllerExpectations) DeleteExpectations(controllerKey string) {
	if exp, exists, err := r.GetByKey(controllerKey); err == nil && exists {
		if err := r.Delete(exp); err != nil {
			klog.V(2).Infof("Error deleting expectations for controller %v: %v", controllerKey, err)
		}
	}
}

// SatisfiedExpectations returns true if the required adds/dels for the given controller have been observed.
// Add/del counts are established by the controller at sync time, and updated as controllees are observed by the controller
// manager.
func (r *ControllerExpectations) SatisfiedExpectations(controllerKey string) bool {
	if exp, exists, err := r.GetExpectations(controllerKey); exists {
		if exp.Fulfilled() {
			r.logger.V(4).Infof("Controller expectations fulfilled %s", controllerKey)
			return true
		} else if exp.isExpired() {
			r.logger.Errorf("expectation expired for key %s", controllerKey)
			panic(fmt.Sprintf("expected panic for expectation [%s] timeout for key %s", r.name, controllerKey))
		} else {
			r.logger.V(4).Infof("Controller still waiting on expectations %#v", controllerKey)
			return false
		}
	} else if err != nil {
		klog.V(2).Infof("Error encountered while checking expectations %#v, forcing sync", err)
	} else {
		// When a new controller is created, it doesn't have expectations.
		// When it doesn't see expected watch events for > TTL, the expectations expire.
		//	- In this case it wakes up, creates/deletes controllees, and sets expectations again.
		// When it has satisfied expectations and no controllees need to be created/destroyed > TTL, the expectations expire.
		//	- In this case it continues without setting expectations till it needs to create/delete controllees.
		klog.V(4).Infof("Controller %v either never recorded expectations, or the ttl expired.", controllerKey)
	}
	// Trigger a sync if we either encountered and error (which shouldn't happen since we're
	// getting from local store) or this controller hasn't established expectations.
	return true
}

// TODO: Extend ExpirationCache to support explicit expiration.
// TODO: Make this possible to disable in tests.
// TODO: Support injection of clock.
func (exp *ControlleeExpectations) isExpired() bool {
	return time.Since(exp.timestamp) > ExpectationsTimeout
}

// Not thread safe
func (r *ControllerExpectations) InitExpectations(controllerKey string) error {
	if _, exist, err := r.GetByKey(controllerKey); err != nil {
		return err
	} else if exist {
		return nil
	}

	klog.V(4).Infof("Setting expectations %s", controllerKey)
	return r.Add(&ControlleeExpectations{add: int64(0), del: int64(0), key: controllerKey, timestamp: time.Now()})
}

// SetExpectations registers new expectations for the given controller. Forgets existing expectations.
func (r *ControllerExpectations) SetExpectations(controllerKey string, add, del int) error {
	exp := &ControlleeExpectations{add: int64(add), del: int64(del), key: controllerKey, timestamp: time.Now()}
	klog.V(4).Infof("Setting expectations %#v", exp)
	return r.Add(exp)
}

func (r *ControllerExpectations) ExpectCreations(controllerKey string, adds int) error {
	return r.SetExpectations(controllerKey, adds, 0)
}

func (r *ControllerExpectations) ExpectDeletions(controllerKey string, dels int) error {
	return r.SetExpectations(controllerKey, 0, dels)
}

// Decrements the expectation counts of the given controller.
func (r *ControllerExpectations) LowerExpectations(controllerKey string, add, del int) {
	if exp, exists, err := r.GetExpectations(controllerKey); err == nil && exists {
		exp.Add(int64(-add), int64(-del))
		// The expectations might've been modified since the update on the previous line.
		klog.V(4).Infof("Lowered expectations %#v", exp)
	}
}

// Increments the expectation counts of the given controller.
func (r *ControllerExpectations) RaiseExpectations(controllerKey string, add, del int) {
	if exp, exists, err := r.GetExpectations(controllerKey); err == nil && exists {
		exp.Add(int64(add), int64(del))
		// The expectations might've been modified since the update on the previous line.
		klog.V(4).Infof("Raised expectations %#v", exp)
	}
}

// CreationObserved atomically decrements the `add` expectation count of the given controller.
func (r *ControllerExpectations) CreationObserved(controllerKey string) {
	r.LowerExpectations(controllerKey, 1, 0)
}

// DeletionObserved atomically decrements the `del` expectation count of the given controller.
func (r *ControllerExpectations) DeletionObserved(controllerKey string) {
	r.LowerExpectations(controllerKey, 0, 1)
}

// Expectations are either fulfilled, or expire naturally.
type Expectations interface {
	Fulfilled() bool
}

// ControlleeExpectations track controllee creates/deletes.
type ControlleeExpectations struct {
	// Important: Since these two int64 fields are using sync/atomic, they have to be at the top of the struct due to a bug on 32-bit platforms
	// See: https://golang.org/pkg/sync/atomic/ for more information
	add       int64
	del       int64
	key       string
	timestamp time.Time
}

// Add increments the add and del counters.
func (e *ControlleeExpectations) Add(add, del int64) {
	atomic.AddInt64(&e.add, add)
	atomic.AddInt64(&e.del, del)
}

// Fulfilled returns true if this expectation has been fulfilled.
func (e *ControlleeExpectations) Fulfilled() bool {
	// TODO: think about why this line being atomic doesn't matter
	return atomic.LoadInt64(&e.add) <= 0 && atomic.LoadInt64(&e.del) <= 0
}

// GetExpectations returns the add and del expectations of the controllee.
func (e *ControlleeExpectations) GetExpectations() (int64, int64) {
	return atomic.LoadInt64(&e.add), atomic.LoadInt64(&e.del)
}

// NewControllerExpectations returns a store for ControllerExpectations.
func NewControllerExpectations(name string, logger *log.Logger) *ControllerExpectations {
	return &ControllerExpectations{cache.NewStore(ExpKeyFunc), name, logger}
}

// Expectations are a way for controllers to tell the controller manager what they expect. eg:
//	ControllerExpectations: {
//		controller1: expects  2 adds in 2 minutes
//		controller2: expects  2 dels in 2 minutes
//		controller3: expects -1 adds in 2 minutes => controller3's expectations have already been met
//	}
//
// Implementation:
//	ControlleeExpectation = pair of atomic counters to track controllee's creation/deletion
//	ControllerExpectationsStore = TTLStore + a ControlleeExpectation per controller
//
// * Once set expectations can only be lowered
// * A controller isn't synced till its expectations are either fulfilled, or expire
// * Controllers that don't set expectations will get woken up for every matching controllee

// ExpKeyFunc to parse out the key from a ControlleeExpectation
var ExpKeyFunc = func(obj interface{}) (string, error) {
	if e, ok := obj.(*ControlleeExpectations); ok {
		return e.key, nil
	}
	if e, ok := obj.(*ResourceVersionExpectationItem); ok {
		return e.key, nil
	}
	panic(fmt.Sprintf("Could not find key for obj %#v", obj))
}
