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

package restart

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

var (
	OpsLifecycleAdapter = &OperationJobRestartOpsLifecycleAdapter{}
)

// OperationJobRestartOpsLifecycleAdapter tells PodOpsLifecycle the container restart ops info
type OperationJobRestartOpsLifecycleAdapter struct{}

// GetID indicates ID of one PodOpsLifecycle
func (a *OperationJobRestartOpsLifecycleAdapter) GetID() string {
	return "operationjob"
}

// GetType indicates type for an Operator
func (a *OperationJobRestartOpsLifecycleAdapter) GetType() podopslifecycle.OperationType {
	return podopslifecycle.OpsLifecycleTypeRestart
}

// AllowMultiType indicates whether multiple IDs which have the same Type are allowed
func (a *OperationJobRestartOpsLifecycleAdapter) AllowMultiType() bool {
	return true
}

// WhenBegin will be executed when begin a lifecycle
func (a *OperationJobRestartOpsLifecycleAdapter) WhenBegin(_ client.Object) (bool, error) {
	return false, nil
}

// WhenFinish will be executed when finish a lifecycle
func (a *OperationJobRestartOpsLifecycleAdapter) WhenFinish(_ client.Object) (bool, error) {
	return false, nil
}
