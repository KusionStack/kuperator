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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceContextSpec defines the desired state of ResourceContext
type ResourceContextSpec struct {
	Contexts []ContextDetail `json:"contexts,omitempty"`
}

type ContextDetail struct {
	ID   int               `json:"id"`
	Data map[string]string `json:"data,omitempty"`
}

//+kubebuilder:object:root=true

// ResourceContext is the Schema for the resourcecontext API
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=rc
type ResourceContext struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ResourceContextSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// ResourceContextList contains a list of ResourceContext
type ResourceContextList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceContext `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ResourceContext{}, &ResourceContextList{})
}

// Contains is used to check whether the key-value pair in contained in Data.
func (cd *ContextDetail) Contains(key, value string) bool {
	if cd.Data == nil {
		return false
	}

	return cd.Data[key] == value
}

// Put is used to specify the value with the specified key in Data .
func (cd *ContextDetail) Put(key, value string) {
	if cd.Data == nil {
		cd.Data = map[string]string{}
	}

	cd.Data[key] = value
}

// Remove is used to remove the specified key from Data .
func (cd *ContextDetail) Remove(key string) {
	if cd.Data == nil {
		return
	}

	delete(cd.Data, key)
}
