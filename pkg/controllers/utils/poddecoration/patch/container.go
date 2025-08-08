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

func AddInitContainers(pod *corev1.Pod, initContainers []*corev1.Container) {
	exists := sets.NewString()
	for _, container := range pod.Spec.InitContainers {
		exists.Insert(container.Name)
	}

	for i, container := range initContainers {
		if exists.Has(container.Name) {
			continue
		}
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, *initContainers[i])
		exists.Insert(container.Name)
	}
}

func PrimaryContainerPatch(pod *corev1.Pod, patches []*appsv1alpha1.PrimaryContainerPatch) {
	for i, patch := range patches {
		switch patch.TargetPolicy {
		case appsv1alpha1.InjectByName, "":
			for idx := range pod.Spec.Containers {
				if patch.Name != nil && pod.Spec.Containers[idx].Name == *patch.Name {
					patchContainer(&pod.Spec.Containers[idx], &patches[i].PodDecorationPrimaryContainer)
					break
				}
			}
		case appsv1alpha1.InjectAllContainers:
			for idx := range pod.Spec.Containers {
				patchContainer(&pod.Spec.Containers[idx], &patches[i].PodDecorationPrimaryContainer)
			}
		case appsv1alpha1.InjectFirstContainer:
			patchContainer(&pod.Spec.Containers[0], &patches[i].PodDecorationPrimaryContainer)
		case appsv1alpha1.InjectLastContainer:
			patchContainer(&pod.Spec.Containers[len(pod.Spec.Containers)-1], &patches[i].PodDecorationPrimaryContainer)
		}
	}
}

func ContainersPatch(pod *corev1.Pod, patches []*appsv1alpha1.ContainerPatch) {
	var beforeContainers, afterContainers []corev1.Container
	for i, patch := range patches {
		switch patch.InjectPolicy {
		case appsv1alpha1.BeforePrimaryContainer:
			beforeContainers = append(beforeContainers, patches[i].Container)
		case appsv1alpha1.AfterPrimaryContainer, "":
			afterContainers = append(afterContainers, patches[i].Container)
		}
	}
	if len(beforeContainers) > 0 {
		pod.Spec.Containers = append(beforeContainers, pod.Spec.Containers...)
	}
	if len(afterContainers) > 0 {
		pod.Spec.Containers = append(pod.Spec.Containers, afterContainers...)
	}
}

func patchContainer(origin *corev1.Container, patch *appsv1alpha1.PodDecorationPrimaryContainer) {
	if patch.Image != nil && *patch.Image != origin.Image {
		origin.Image = *patch.Image
	}
	if len(patch.Env) > 0 {
		origin.Env = MergeEnvByOverwrite(origin.Env, patch.Env)
	}
	if len(patch.VolumeMounts) > 0 {
		origin.VolumeMounts = MergeVolumeMountByOverwrite(origin.VolumeMounts, patch.VolumeMounts)
	}
}

func MergeEnvByOverwrite(original []corev1.EnvVar, additional []corev1.EnvVar) []corev1.EnvVar {
	existsIdx := map[string]int{}
	for i, env := range original {
		existsIdx[env.Name] = i
	}
	for _, env := range additional {
		if idx, ok := existsIdx[env.Name]; ok {
			original[idx] = env
			continue
		}
		original = append(original, env)
	}
	return original
}

func MergeVolumeMountByOverwrite(original []corev1.VolumeMount, additional []corev1.VolumeMount) (res []corev1.VolumeMount) {
	existsIdx := map[string]int{}
	for i, env := range original {
		existsIdx[env.Name] = i
	}
	for _, volumeMount := range additional {
		if idx, ok := existsIdx[volumeMount.Name]; ok {
			original[idx] = volumeMount
			continue
		}
		original = append(original, volumeMount)
	}
	return original
}
