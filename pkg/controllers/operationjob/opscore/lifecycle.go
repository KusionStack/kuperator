/*
Copyright 2024 The KusionStack Authors.

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

package opscore

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
)

type GenericLifecycleAdapter struct {
	ID   string
	Type podopslifecycle.OperationType
}

func (g GenericLifecycleAdapter) GetID() string {
	return g.ID
}

func (g GenericLifecycleAdapter) GetType() podopslifecycle.OperationType {
	return g.Type
}

func (g GenericLifecycleAdapter) AllowMultiType() bool {
	return true
}

func (g GenericLifecycleAdapter) WhenBegin(_ client.Object) (bool, error) {
	return false, nil
}

func (g GenericLifecycleAdapter) WhenFinish(_ client.Object) (bool, error) {
	return false, nil
}

func NewLifecycleAdapter(lifecycleID, lifecycleType string) podopslifecycle.LifecycleAdapter {
	return &GenericLifecycleAdapter{
		ID:   lifecycleID,
		Type: podopslifecycle.OperationType(lifecycleType),
	}
}
