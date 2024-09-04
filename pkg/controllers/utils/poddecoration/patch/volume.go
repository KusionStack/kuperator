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
)

func MergeWithOverwriteVolumes(original []corev1.Volume, additional []corev1.Volume) []corev1.Volume {
	volumeMap := map[string]*corev1.Volume{}
	for i, volume := range additional {
		volumeMap[volume.Name] = &additional[i]
	}
	for i, volume := range original {
		if added, ok := volumeMap[volume.Name]; ok {
			original[i].VolumeSource = added.VolumeSource
			delete(volumeMap, original[i].Name)
		}
	}
	for _, volume := range additional {
		if added, ok := volumeMap[volume.Name]; ok {
			original = append(original, *added)
		}
	}
	return original
}

func MergeVolumes(original []corev1.Volume, additional []corev1.Volume) []corev1.Volume {
	exists := sets.NewString()
	for _, volume := range original {
		exists.Insert(volume.Name)
	}

	for _, volume := range additional {
		if exists.Has(volume.Name) {
			continue
		}
		original = append(original, volume)
		exists.Insert(volume.Name)
	}

	return original
}
