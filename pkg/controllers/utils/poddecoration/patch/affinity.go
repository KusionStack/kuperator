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

package patch

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func PatchAffinity(pod *corev1.Pod, affinity *appsv1alpha1.PodDecorationAffinity) {
	if affinity.OverrideAffinity != nil {
		pod.Spec.Affinity = affinity.OverrideAffinity
	}
	if len(affinity.NodeSelectorTerms) > 0 {
		if pod.Spec.Affinity == nil {
			pod.Spec.Affinity = &corev1.Affinity{}
		}
		if pod.Spec.Affinity.NodeAffinity == nil {
			pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
		}
		if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
		}
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(
			pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, affinity.NodeSelectorTerms...)
	}
}

func MergeTolerations(original []corev1.Toleration, additional []corev1.Toleration) []corev1.Toleration {
	exists := sets.NewString()
	for _, toleration := range original {
		exists.Insert(toleration.Key)
	}

	for _, toleration := range additional {
		if exists.Has(toleration.Key) {
			continue
		}
		original = append(original, toleration)
		exists.Insert(toleration.Key)
	}
	return original
}

func MergeWithOverwriteTolerations(original []corev1.Toleration, additional []corev1.Toleration) []corev1.Toleration {
	additionalMap := map[string]*corev1.Toleration{}
	for i, toleration := range additional {
		additionalMap[toleration.Key] = &additional[i]
	}
	for i, toleration := range original {
		rep, ok := additionalMap[toleration.Key]
		if ok {
			original[i] = *rep
			delete(additionalMap, toleration.Key)
		}
	}
	for _, add := range additionalMap {
		original = append(original, *add)
	}
	return original
}
