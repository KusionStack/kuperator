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
	"context"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type ActionHandler interface {
	// OperateTarget do real operation to target, with protected by podOpsLifecycle
	OperateTarget(context.Context, client.Client, *appsv1alpha1.OperationJob, record.EventRecorder, *OpsCandidate) error

	// GetOpsProgress returns target's current opsStatus, e.g., progress, reason, message
	GetOpsProgress(context.Context, client.Client, *appsv1alpha1.OperationJob, record.EventRecorder, *OpsCandidate) (
		progress appsv1alpha1.OperationProgress, reason string, message string, err error)

	// ReleaseTarget releases the target from operation when the operationJob is deleted
	ReleaseTarget(context.Context, client.Client, *appsv1alpha1.OperationJob, record.EventRecorder, *OpsCandidate) error
}
