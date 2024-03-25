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
	"sigs.k8s.io/controller-runtime/pkg/event"

	"kusionstack.io/operating/pkg/utils"
)

type PodPredicate struct {
}

// Create returns true if the Create event should be processed
func (p *PodPredicate) Create(e event.CreateEvent) bool {
	return utils.ControlledByKusionStack(e.Object)
}

// Delete returns true if the Delete event should be processed
func (p *PodPredicate) Delete(e event.DeleteEvent) bool {
	return utils.ControlledByKusionStack(e.Object)
}

// Update returns true if the Update event should be processed
func (p *PodPredicate) Update(e event.UpdateEvent) bool {
	return utils.ControlledByKusionStack(e.ObjectNew) || utils.ControlledByKusionStack(e.ObjectOld)
}

// Generic returns true if the Generic event should be processed
func (p *PodPredicate) Generic(e event.GenericEvent) bool {
	return utils.ControlledByKusionStack(e.Object)
}
