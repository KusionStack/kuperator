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

package revision

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func getPodDecorationPatch(pd *appsv1alpha1.PodDecoration) ([]byte, error) {
	dsBytes, err := json.Marshal(pd)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	err = json.Unmarshal(dsBytes, &raw)
	if err != nil {
		return nil, err
	}
	objCopy := make(map[string]interface{})
	specCopy := make(map[string]interface{})

	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	weight := spec["weight"]

	template["$patch"] = "replace"
	specCopy["template"] = template
	specCopy["weight"] = weight
	objCopy["spec"] = specCopy
	patch, err := json.Marshal(objCopy)
	return patch, err
}

type RevisionOwnerAdapter struct {
}

func (roa *RevisionOwnerAdapter) GetGroupVersionKind() schema.GroupVersionKind {
	return appsv1alpha1.SchemeGroupVersion.WithKind("PodDecoration")
}

func (roa *RevisionOwnerAdapter) GetMatchLabels(obj metav1.Object) map[string]string {
	pd, _ := obj.(*appsv1alpha1.PodDecoration)
	selector := pd.Spec.Selector
	if selector == nil {
		return nil
	}
	return selector.MatchLabels
}

func (roa *RevisionOwnerAdapter) GetCollisionCount(obj metav1.Object) *int32 {
	pd, _ := obj.(*appsv1alpha1.PodDecoration)
	return &pd.Status.CollisionCount
}

func (roa *RevisionOwnerAdapter) GetHistoryLimit(obj metav1.Object) int32 {
	pd, _ := obj.(*appsv1alpha1.PodDecoration)
	return pd.Spec.HistoryLimit
}

func (roa *RevisionOwnerAdapter) GetPatch(obj metav1.Object) ([]byte, error) {
	cs, _ := obj.(*appsv1alpha1.PodDecoration)
	return getPodDecorationPatch(cs)
}

func (roa *RevisionOwnerAdapter) GetCurrentRevision(obj metav1.Object) string {
	pd, _ := obj.(*appsv1alpha1.PodDecoration)
	return pd.Status.CurrentRevision
}

func (roa *RevisionOwnerAdapter) GetInUsedRevisions(_ metav1.Object) (sets.String, error) {
	return sets.NewString(), nil
}
