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
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsalphav1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func getPodDecorationPatch(pd *appsalphav1.PodDecoration) ([]byte, error) {
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
	injectStrategyCopy := make(map[string]interface{})

	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	injectStrategy := spec["injectStrategy"].(map[string]interface{})

	group := injectStrategy["group"]
	injectStrategyCopy["group"] = group
	specCopy["injectStrategy"] = injectStrategyCopy
	template["$patch"] = "replace"
	specCopy["template"] = template
	objCopy["spec"] = specCopy
	patch, err := json.Marshal(objCopy)
	return patch, err
}

type revisionOwnerAdapter struct {
}

func (roa *revisionOwnerAdapter) GetSelector(obj metav1.Object) *metav1.LabelSelector {
	ips, _ := obj.(*appsalphav1.PodDecoration)
	return ips.Spec.Selector
}

func (roa *revisionOwnerAdapter) GetCollisionCount(obj metav1.Object) *int32 {
	ips, _ := obj.(*appsalphav1.PodDecoration)
	return &ips.Status.CollisionCount
}

func (roa *revisionOwnerAdapter) GetHistoryLimit(obj metav1.Object) int32 {
	ips, _ := obj.(*appsalphav1.PodDecoration)
	return ips.Spec.HistoryLimit
}

func (roa *revisionOwnerAdapter) GetPatch(obj metav1.Object) ([]byte, error) {
	cs, _ := obj.(*appsalphav1.PodDecoration)
	return getPodDecorationPatch(cs)
}

func (roa *revisionOwnerAdapter) GetSelectorLabels(obj metav1.Object) map[string]string {
	return map[string]string{
		appsalphav1.PodDecorationControllerRevisionOwner: obj.GetName(),
	}
}

func (roa *revisionOwnerAdapter) GetCurrentRevision(obj metav1.Object) string {
	ips, _ := obj.(*appsalphav1.PodDecoration)
	return ips.Status.CurrentRevision
}

func (roa *revisionOwnerAdapter) IsInUsed(_ metav1.Object, _ string) bool {
	return false
}
