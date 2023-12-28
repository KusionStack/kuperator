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

package collaset

import (
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsalphav1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

var _ handler.EventHandler = &podDecorationHandler{}

type podDecorationHandler struct {
}

// Create implements EventHandler.
func (e *podDecorationHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
}

// Update implements EventHandler.
func (e *podDecorationHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	pdOld := evt.ObjectOld.(*appsalphav1.PodDecoration)
	pdNew := evt.ObjectNew.(*appsalphav1.PodDecoration)

	if !reflect.DeepEqual(pdOld.Status, pdNew.Status) {
		for _, detail := range pdNew.Status.Details {
			q.Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pdNew.Namespace,
					Name:      detail.CollaSet,
				},
			})
		}
	}
}

// Delete implements EventHandler.
func (e *podDecorationHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
}

// Generic implements EventHandler.
func (e *podDecorationHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
}
