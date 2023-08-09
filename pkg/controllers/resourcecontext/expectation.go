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

package resourcecontext

import (
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"kusionstack.io/kafed/pkg/controllers/utils/expectations"
)

var (
	// activeExpectations is used to check the cache in informer is updated, before reconciling.
	activeExpectations *expectations.ActiveExpectations
)

func InitExpectations(c client.Client) {
	activeExpectations = expectations.NewActiveExpectations(c)
}

type ExpectationEventHandler struct {
}

// Create is called in response to an create event - e.g. Pod Creation.
func (h *ExpectationEventHandler) Create(event.CreateEvent, workqueue.RateLimitingInterface) {}

// Update is called in response to an update event -  e.g. Pod Updated.
func (h *ExpectationEventHandler) Update(event.UpdateEvent, workqueue.RateLimitingInterface) {}

// Delete is called in response to a delete event - e.g. Pod Deleted.
func (h *ExpectationEventHandler) Delete(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
	activeExpectations.Delete(e.Object.GetNamespace(), e.Object.GetName())
}

// Generic is called in response to an event of an unknown type or a synthetic event triggered as a cron or
// external trigger request - e.g. reconcile Autoscaling, or a Webhook.
func (h *ExpectationEventHandler) Generic(event.GenericEvent, workqueue.RateLimitingInterface) {}
