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
	jsonpatch "github.com/evanphx/json-patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

// PatchMetadata patch annotations and labels
func PatchMetadata(oldMetadata *metav1.ObjectMeta, patches []*appsv1alpha1.PodDecorationPodTemplateMeta) (err error) {
	if oldMetadata.Annotations == nil {
		oldMetadata.Annotations = map[string]string{}
	}
	if oldMetadata.Labels == nil {
		oldMetadata.Labels = map[string]string{}
	}
	for _, patch := range patches {
		switch patch.PatchPolicy {
		case appsv1alpha1.RetainMetadata, "":
			retainPatchMetadata(oldMetadata, patch)
		case appsv1alpha1.OverwriteMetadata:
			overwritePatchMetadata(oldMetadata, patch)
		case appsv1alpha1.MergePatchJsonMetadata:
			if err = mergePatchJsonMetadata(oldMetadata, patch); err != nil {
				return
			}
		}
	}
	return
}

func retainPatchMetadata(metadata *metav1.ObjectMeta, patch *appsv1alpha1.PodDecorationPodTemplateMeta) {
	for k, v := range patch.Annotations {
		if _, ok := metadata.Annotations[k]; !ok {
			metadata.Annotations[k] = v
		}
	}
	for k, v := range patch.Labels {
		if _, ok := metadata.Labels[k]; !ok {
			metadata.Labels[k] = v
		}
	}
}

func overwritePatchMetadata(metadata *metav1.ObjectMeta, patch *appsv1alpha1.PodDecorationPodTemplateMeta) {
	for k, v := range patch.Annotations {
		metadata.Annotations[k] = v
	}
	for k, v := range patch.Labels {
		metadata.Labels[k] = v
	}
}

func mergePatchJsonMetadata(metadata *metav1.ObjectMeta, patch *appsv1alpha1.PodDecorationPodTemplateMeta) error {
	for key, patchValue := range patch.Annotations {
		oldValue := metadata.Annotations[key]
		if oldValue == "" {
			metadata.Annotations[key] = patchValue
			continue
		}
		newValue, err := jsonpatch.MergePatch([]byte(oldValue), []byte(patchValue))
		if err != nil {
			return err
		}
		metadata.Annotations[key] = string(newValue)
	}
	return nil
}
