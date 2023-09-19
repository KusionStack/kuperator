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

package poddeletion

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type PredicateDeletionIndicatedPod struct {
}

// Create returns true if the Create event should be processed
func (p *PredicateDeletionIndicatedPod) Create(e event.CreateEvent) bool {
	return hasTerminatingLabel(e.Object)
}

// Delete returns true if the Delete event should be processed
func (p *PredicateDeletionIndicatedPod) Delete(e event.DeleteEvent) bool {
	return hasTerminatingLabel(e.Object)
}

// Update returns true if the Update event should be processed
func (p *PredicateDeletionIndicatedPod) Update(e event.UpdateEvent) bool {
	return hasTerminatingLabel(e.ObjectNew)
}

// Generic returns true if the Generic event should be processed
func (p *PredicateDeletionIndicatedPod) Generic(e event.GenericEvent) bool {
	return hasTerminatingLabel(e.Object)
}

func hasTerminatingLabel(pod client.Object) bool {
	if pod.GetLabels() == nil {
		return false
	}

	if _, exist := pod.GetLabels()[appsv1alpha1.PodDeletionIndicationLabelKey]; exist {
		return true
	}

	return false
}
