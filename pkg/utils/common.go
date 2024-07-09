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

package utils

import (
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/operating/apis/apps/v1alpha1"
)

// DumpJSON returns the JSON encoding
func DumpJSON(o interface{}) string {
	j, _ := json.Marshal(o)
	return string(j)
}

func ControlledByKusionStack(obj client.Object) bool {
	if obj == nil || obj.GetLabels() == nil {
		return false
	}
	v, ok := obj.GetLabels()[v1alpha1.ControlledByKusionStackLabelKey]
	return ok && v == "true"
}

func ControlByKusionStack(obj client.Object) {
	if obj.GetLabels() == nil {
		obj.SetLabels(map[string]string{})
	}

	if v, ok := obj.GetLabels()[v1alpha1.ControlledByKusionStackLabelKey]; !ok || v != "true" {
		obj.GetLabels()[v1alpha1.ControlledByKusionStackLabelKey] = "true"
	}
}
