/*
Copyright 2016 The Kubernetes Authors.
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

package utils

import (
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type RefManager struct {
	client   client.Writer
	selector labels.Selector
	owner    metav1.Object
	schema   *runtime.Scheme

	once        sync.Once
	canAdoptErr error
}

func NewRefManager(client client.Writer, selector *metav1.LabelSelector, owner metav1.Object, schema *runtime.Scheme) (*RefManager, error) {
	s, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	return &RefManager{
		client:   client,
		selector: s,
		owner:    owner,
		schema:   schema,
	}, nil
}

func (mgr *RefManager) ClaimOwned(objs []client.Object) ([]client.Object, error) {
	match := func(obj metav1.Object) bool {
		return mgr.selector.Matches(labels.Set(obj.GetLabels()))
	}

	var claimObjs []client.Object
	var errList []error
	for _, obj := range objs {
		ok, err := mgr.ClaimObject(obj, match)
		if err != nil {
			errList = append(errList, err)
			continue
		}
		if ok {
			claimObjs = append(claimObjs, obj)
		}
	}
	return claimObjs, utilerrors.NewAggregate(errList)
}

func (mgr *RefManager) canAdoptOnce() error {
	mgr.once.Do(func() {
		mgr.canAdoptErr = mgr.canAdopt()
	})

	return mgr.canAdoptErr
}

func (mgr *RefManager) canAdopt() error {
	if mgr.owner.GetDeletionTimestamp() != nil {
		return fmt.Errorf("%v/%v has just been deleted at %v",
			mgr.owner.GetNamespace(), mgr.owner.GetName(), mgr.owner.GetDeletionTimestamp())
	}

	return nil
}

func (mgr *RefManager) adopt(obj client.Object) error {
	if err := mgr.canAdoptOnce(); err != nil {
		return fmt.Errorf("can't adopt Object %v/%v (%v): %v", obj.GetNamespace(), obj.GetName(), obj.GetUID(), err)
	}

	if mgr.schema == nil {
		return fmt.Errorf("schema should not be nil")
	}

	if err := controllerutil.SetControllerReference(mgr.owner, obj, mgr.schema); err != nil {
		return fmt.Errorf("can't set Object %v/%v (%v) owner reference: %v", obj.GetNamespace(), obj.GetName(), obj.GetUID(), err)
	}

	return mgr.client.Update(context.TODO(), obj)
}

func (mgr *RefManager) release(obj client.Object) error {
	idx := -1
	for i, ref := range obj.GetOwnerReferences() {
		if ref.UID == mgr.owner.GetUID() {
			idx = i
			break
		}
	}
	if idx > -1 {
		obj.SetOwnerReferences(append(obj.GetOwnerReferences()[:idx], obj.GetOwnerReferences()[idx+1:]...))
		if err := mgr.client.Update(context.TODO(), obj); err != nil {
			return err
		}
	}

	return nil
}

// ClaimObject tries to take ownership of an object for this controller.
//
// It will reconcile the following:
//   - Adopt orphans if the match function returns true.
//   - Release owned objects if the match function returns false.
//
// A non-nil error is returned if some form of reconciliation was attempted and
// failed. Usually, controllers should try again later in case reconciliation
// is still needed.
//
// If the error is nil, either the reconciliation succeeded, or no
// reconciliation was necessary. The returned boolean indicates whether you now
// own the object.
//
// No reconciliation will be attempted if the controller is being deleted.
func (mgr *RefManager) ClaimObject(obj client.Object, match func(metav1.Object) bool) (bool, error) {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef != nil {
		if controllerRef.UID != mgr.owner.GetUID() {
			// Owned by someone else. Ignore.
			return false, nil
		}
		if match(obj) {
			// We already own it and the selector matches.
			// Return true (successfully claimed) before checking deletion timestamp.
			// We're still allowed to claim things we already own while being deleted
			// because doing so requires taking no actions.
			return true, nil
		}
		// Owned by us but selector doesn't match.
		// Try to release, unless we're being deleted.
		if mgr.owner.GetDeletionTimestamp() != nil {
			return false, nil
		}
		if err := mgr.release(obj); err != nil {
			// If the pod no longer exists, ignore the error.
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			// Either someone else released it, or there was a transient error.
			// The controller should requeue and try again if it's still stale.
			return false, err
		}
		// Successfully released.
		return false, nil
	}

	// It's an orphan.
	if mgr.owner.GetDeletionTimestamp() != nil || !match(obj) {
		// Ignore if we're being deleted or selector doesn't match.
		return false, nil
	}
	if obj.GetDeletionTimestamp() != nil {
		// Ignore if the object is being deleted
		return false, nil
	}
	// Selector matches. Try to adopt.
	if err := mgr.adopt(obj); err != nil {
		// If the pod no longer exists, ignore the error.
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		// Either someone else claimed it first, or there was a transient error.
		// The controller should requeue and try again if it's still orphaned.
		return false, err
	}
	// Successfully adopted.
	return true, nil
}
