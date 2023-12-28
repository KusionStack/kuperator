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

package utils

import (
	appsv1 "k8s.io/api/apps/v1"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type RelatedResources struct {
	Revisions       []*appsv1.ControllerRevision
	CurrentRevision *appsv1.ControllerRevision
	UpdatedRevision *appsv1.ControllerRevision

	PDGetter PodDecorationGetter

	NewStatus *appsv1alpha1.CollaSetStatus
}
