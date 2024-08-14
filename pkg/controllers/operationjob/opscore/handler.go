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

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/operating/pkg/utils/mixin"
)

type ActionProgress string

const (
	ActionProgressProcessing ActionProgress = "Processing"
	ActionProgressFailed     ActionProgress = "Failed"
	ActionProgressSucceeded  ActionProgress = "Succeeded"
)

type ActionHandler interface {
	// SetUp sets up action with manager in AddToMgr, i.e., watch, cache...
	SetUp(controller.Controller, ctrl.Manager, *mixin.ReconcilerMixin) error

	// OperateTarget do real operation to target
	OperateTarget(context.Context, *OpsCandidate, *appsv1alpha1.OperationJob) error

	// GetOpsProgress returns target's current opsStatus, e.g., progress, reason, message
	GetOpsProgress(context.Context, *OpsCandidate, *appsv1alpha1.OperationJob) (progress ActionProgress, err error)

	// ReleaseTarget releases the target from operation when the operationJob is deleted
	ReleaseTarget(context.Context, *OpsCandidate, *appsv1alpha1.OperationJob) error
}
