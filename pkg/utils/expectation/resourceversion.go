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

package expectation

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type ResourceVersionExpectation struct {
	cache.Store
}

func NewResourceVersionExpectation() *ResourceVersionExpectation {
	return &ResourceVersionExpectation{cache.NewStore(ExpKeyFunc)}
}

func (r *ResourceVersionExpectation) GetExpectations(controllerKey string) (*ResourceVersionExpectationItem, bool, error) {
	if exp, exists, err := r.GetByKey(controllerKey); err == nil && exists {
		return exp.(*ResourceVersionExpectationItem), true, nil
	} else {
		return nil, false, err
	}
}

func (r *ResourceVersionExpectation) DeleteExpectations(controllerKey string) {
	if exp, exists, err := r.GetByKey(controllerKey); err == nil && exists {
		if err := r.Delete(exp); err != nil {
			klog.V(2).Infof("Error deleting expectations for controller %v: %v", controllerKey, err)
		}
	}
}

func (r *ResourceVersionExpectation) SatisfiedExpectations(controllerKey string, resourceVersion string) bool {
	if exp, exists, err := r.GetExpectations(controllerKey); exists {
		if exp.Fulfilled(resourceVersion) {
			klog.V(4).Infof("Accuracy expectations fulfilled %s", controllerKey)
			return true
		} else if exp.isExpired() {
			klog.Errorf("Accuracy expectation expired for key %s", controllerKey)
			panic(fmt.Sprintf("expected panic for accuracy expectation timeout for key %s", controllerKey))
		} else {
			klog.V(4).Infof("Controller still waiting on accuracy expectations %s", controllerKey)
			return false
		}
	} else if err != nil {
		klog.V(2).Infof("Error encountered while checking accuracy expectations %#v, forcing sync", err)
	} else {
		// When a new controller is created, it doesn't have expectations.
		// When it doesn't see expected watch events for > TTL, the expectations expire.
		//	- In this case it wakes up, creates/deletes controllees, and sets expectations again.
		// When it has satisfied expectations and no controllees need to be created/destroyed > TTL, the expectations expire.
		//	- In this case it continues without setting expectations till it needs to create/delete controllees.
		klog.V(4).Infof("Accuracy controller %v either never recorded expectations, or the ttl expired.", controllerKey)
	}
	// Trigger a sync if we either encountered and error (which shouldn't happen since we're
	// getting from local store) or this controller hasn't established expectations.
	return true
}

func (r *ResourceVersionExpectation) SetExpectations(controllerKey string, resourceVersion string) error {
	exp := &ResourceVersionExpectationItem{key: controllerKey, timestamp: time.Now()}
	exp.Set(resourceVersion)
	klog.V(4).Infof("Setting expectations %#v", exp)
	return r.Add(exp)
}

func (r *ResourceVersionExpectation) ExpectUpdate(controllerKey string, resourceVersion string) error {
	if exp, exists, err := r.GetExpectations(controllerKey); err != nil {
		return err
	} else if exists {
		exp.Set(resourceVersion)
	} else {
		r.SetExpectations(controllerKey, resourceVersion)
	}
	return nil
}

type ResourceVersionExpectationItem struct {
	lock            sync.RWMutex
	resourceVersion int64
	key             string
	timestamp       time.Time
}

func (i *ResourceVersionExpectationItem) Set(resourceVersion string) {
	i.lock.Lock()
	defer i.lock.Unlock()

	var rv int64
	var err error
	if resourceVersion == "" {
		rv = 0
	} else {
		rv, err = strconv.ParseInt(resourceVersion, 10, 64)
		if err != nil {
			return
		}
	}

	if i.resourceVersion < rv {
		i.resourceVersion = rv
		i.timestamp = time.Now()
	}
}

func (i *ResourceVersionExpectationItem) Fulfilled(resourceVersion string) bool {
	i.lock.RLock()
	defer i.lock.RUnlock()

	var rv int64
	var err error
	if resourceVersion == "" {
		rv = 0
	} else {
		rv, err = strconv.ParseInt(resourceVersion, 10, 64)
		if err != nil {
			return false
		}
	}

	return i.resourceVersion < rv
}

func (i *ResourceVersionExpectationItem) isExpired() bool {
	return time.Since(i.timestamp) > ExpectationsTimeout
}
