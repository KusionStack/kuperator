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

package poddecoration

import (
	corev1 "k8s.io/api/core/v1"

	"kusionstack.io/operating/pkg/utils"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/poddecoration/patch"
)

func PatchPodDecoration(pod *corev1.Pod, template *appsv1alpha1.PodDecorationPodTemplate) (err error) {
	if len(template.Metadata) > 0 {
		err = patch.PatchMetadata(&pod.ObjectMeta, template.Metadata)
	}
	if len(template.InitContainers) > 0 {
		patch.AddInitContainers(pod, template.InitContainers)
	}

	if len(template.PrimaryContainers) > 0 {
		patch.PrimaryContainerPatch(pod, template.PrimaryContainers)
	}

	if len(template.Containers) > 0 {
		patch.ContainersPatch(pod, template.Containers)
	}

	if len(template.Volumes) > 0 {
		pod.Spec.Volumes = patch.MergeVolumes(pod.Spec.Volumes, template.Volumes)
	}

	if template.Affinity != nil {
		patch.PatchAffinity(pod, template.Affinity)
	}

	if template.Tolerations != nil {
		pod.Spec.Tolerations = patch.MergeTolerations(pod.Spec.Tolerations, template.Tolerations)
	}
	return
}

func PatchListOfDecorations(pod *corev1.Pod, podDecorations map[string]*appsv1alpha1.PodDecoration) (err error) {
	for _, pd := range podDecorations {
		if patchErr := PatchPodDecoration(pod, &pd.Spec.Template); patchErr != nil {
			err = utils.Join(err, patchErr)
		}
	}
	setDecorationInfo(pod, podDecorations)
	return
}
