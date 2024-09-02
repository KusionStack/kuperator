/*
Copyright 2024 The KusionStack Authors.

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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

// AllowResourceExclude checks if pod or pvc is allowed to exclude
func AllowResourceExclude(obj metav1.Object, ownerName, ownerKind string) (bool, string) {
	labels := obj.GetLabels()
	// not controlled by ks manager
	if labels == nil {
		return false, "object's label is empty"
	} else if val, exist := labels[appsv1alpha1.ControlledByKusionStackLabelKey]; !exist || val != "true" {
		return false, "object is not controlled by kusionstack system"
	}

	// not controlled by current collaset
	if controller := metav1.GetControllerOf(obj); controller == nil || controller.Name != ownerName || controller.Kind != ownerKind {
		return false, "object is not owned by any one, not allowed to exclude"
	}
	return true, ""
}

// AllowResourceInclude checks if pod or pvc is allowed to include
func AllowResourceInclude(obj metav1.Object, ownerName, ownerKind string) (bool, string) {
	labels := obj.GetLabels()
	ownerRefs := obj.GetOwnerReferences()

	// not controlled by ks manager
	if labels == nil {
		return false, "object's label is empty"
	} else if val, exist := labels[appsv1alpha1.ControlledByKusionStackLabelKey]; !exist || val != "true" {
		return false, "object is not controlled by kusionstack system"
	}

	if ownerRefs != nil {
		if controller := metav1.GetControllerOf(obj); controller != nil {
			// controlled by others
			if controller.Name != ownerName || controller.Kind != ownerKind {
				return false, fmt.Sprintf("object's ownerReference controller is not %s/%s", ownerKind, ownerName)
			}
			// currently being owned but not indicate orphan
			if controller.Name == ownerName && controller.Kind == ownerKind {
				if _, exist := labels[appsv1alpha1.PodOrphanedIndicateLabelKey]; !exist {
					return false, fmt.Sprintf("object's is controlled by %s/%s, but marked as orphaned", ownerKind, ownerName)
				}
			}
		}
	}
	return true, ""
}
