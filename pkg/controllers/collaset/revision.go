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

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/revision"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
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

	// Create a patch of the DaemonSet that replaces spec.template
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

func NewRevisionOwnerAdapter(podControl podcontrol.Interface) revision.OwnerAdapter {
	return &revisionOwnerAdapter{
		podControl: podControl,
	}
}

type revisionOwnerAdapter struct {
	podControl podcontrol.Interface
}

func (roa *revisionOwnerAdapter) GetSelector(obj metav1.Object) *metav1.LabelSelector {
	ips, _ := obj.(*appsv1alpha1.CollaSet)
	return ips.Spec.Selector
}

func (roa *revisionOwnerAdapter) GetCollisionCount(obj metav1.Object) *int32 {
	ips, _ := obj.(*appsv1alpha1.CollaSet)
	return ips.Status.CollisionCount
}

func (roa *revisionOwnerAdapter) GetHistoryLimit(obj metav1.Object) int32 {
	ips, _ := obj.(*appsv1alpha1.CollaSet)
	return ips.Spec.HistoryLimit
}

func (roa *revisionOwnerAdapter) GetPatch(obj metav1.Object) ([]byte, error) {
	cs, _ := obj.(*appsv1alpha1.CollaSet)
	return getCollaSetPatch(cs)
}

func (roa *revisionOwnerAdapter) GetCurrentRevision(obj metav1.Object) string {
	ips, _ := obj.(*appsv1alpha1.CollaSet)
	return ips.Status.CurrentRevision
}

func (roa *revisionOwnerAdapter) IsInUsed(obj metav1.Object, revision string) bool {
	ips, _ := obj.(*appsv1alpha1.CollaSet)
	
	if ips.Status.UpdatedRevision == revision || ips.Status.CurrentRevision == revision {
		return true
	}

	pods, _ := roa.podControl.GetFilteredPods(ips.Spec.Selector, ips)
	for _, pod := range pods { 
		if pod.Labels != nil {
			currentRevisionName, exist := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
			if exist && currentRevisionName == revision {
				return true
			}
		}
	}

	return false
}
