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
	corev1 "k8s.io/api/core/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
)

// ActiveExpectations is used to check the cache in informer is updated, before reconciling.
var ActiveExpectations *expectations.ActiveExpectations

func InitExpectations(c client.Client) {
	ActiveExpectations = expectations.NewActiveExpectations(c)
}

var PodGVK = corev1.SchemeGroupVersion.WithKind("Pod")
var PVCGVK = corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim")
var CollaSetGVK = appsv1alpha1.SchemeGroupVersion.WithKind("CollaSet")
var ResourceContextGVK = appsv1alpha1.SchemeGroupVersion.WithKind("ResourceContext")
