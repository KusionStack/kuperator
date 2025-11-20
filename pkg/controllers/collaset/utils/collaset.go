/*
Copyright 2025 The KusionStack Authors.

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
)

var (
	PodGVK             = corev1.SchemeGroupVersion.WithKind("Pod")
	PVCGVK             = corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim")
	CollaSetGVK        = appsv1alpha1.SchemeGroupVersion.WithKind("CollaSet")
	ResourceContextGVK = appsv1alpha1.SchemeGroupVersion.WithKind("ResourceContext")
)

func PodNamingSuffixPersistentSequence(cls *appsv1alpha1.CollaSet) bool {
	if cls.Spec.NamingStrategy == nil {
		return false
	}
	return cls.Spec.NamingStrategy.PodNamingSuffixPolicy == appsv1alpha1.PodNamingSuffixPolicyPersistentSequence
}
