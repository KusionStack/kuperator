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

package collaset

import (
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/collaset/podcontrol"
)

func getCollaSetPatch(cls *appsv1alpha1.CollaSet) ([]byte, error) {
	dsBytes, err := json.Marshal(cls)
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

	// Create a patch of the CollaSet that replaces spec.template
	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	template["$patch"] = "replace"
	specCopy["template"] = template

	if _, exist := spec["volumeClaimTemplates"]; exist {
		specCopy["volumeClaimTemplates"] = spec["volumeClaimTemplates"]
	}

	objCopy["spec"] = specCopy
	patch, err := json.Marshal(objCopy)
	return patch, err
}

type revisionOwnerAdapter struct {
	podControl podcontrol.Interface
}

func (roa *revisionOwnerAdapter) GetGroupVersionKind() schema.GroupVersionKind {
	return appsv1alpha1.SchemeGroupVersion.WithKind("CollaSet")
}

func (roa *revisionOwnerAdapter) GetMatchLabels(obj metav1.Object) map[string]string {
	cls, _ := obj.(*appsv1alpha1.CollaSet)
	selector := cls.Spec.Selector
	if selector == nil {
		return nil
	}
	return selector.MatchLabels
}

func (roa *revisionOwnerAdapter) GetCollisionCount(obj metav1.Object) *int32 {
	cls, _ := obj.(*appsv1alpha1.CollaSet)
	return cls.Status.CollisionCount
}

func (roa *revisionOwnerAdapter) GetHistoryLimit(obj metav1.Object) int32 {
	cls, _ := obj.(*appsv1alpha1.CollaSet)
	return cls.Spec.HistoryLimit
}

func (roa *revisionOwnerAdapter) GetPatch(obj metav1.Object) ([]byte, error) {
	cs, _ := obj.(*appsv1alpha1.CollaSet)
	return getCollaSetPatch(cs)
}

func (roa *revisionOwnerAdapter) GetCurrentRevision(obj metav1.Object) string {
	cls, _ := obj.(*appsv1alpha1.CollaSet)
	return cls.Status.CurrentRevision
}

func (roa *revisionOwnerAdapter) GetInUsedRevisions(obj metav1.Object) (sets.String, error) {
	inUsed := sets.NewString()
	cls, _ := obj.(*appsv1alpha1.CollaSet)
	if cls.Status.UpdatedRevision != "" {
		inUsed.Insert(cls.Status.UpdatedRevision)
	}
	if cls.Status.CurrentRevision != "" {
		inUsed.Insert(cls.Status.CurrentRevision)
	}
	pods, _ := roa.podControl.GetFilteredPods(cls.Spec.Selector, cls)
	for _, pod := range pods {
		if pod.Labels != nil {
			if currentRevisionName, exist := pod.Labels[appsv1.ControllerRevisionHashLabelKey]; exist {
				inUsed.Insert(currentRevisionName)
			}
		}
	}
	return inUsed, nil
}
