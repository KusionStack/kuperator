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

	"sigs.k8s.io/controller-runtime/pkg/controller"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/utils/mixin"
)

type ActionProgress string

const (
	ActionProgressProcessing ActionProgress = "Processing"
	ActionProgressFailed     ActionProgress = "Failed"
	ActionProgressSucceeded  ActionProgress = "Succeeded"
)

type ActionHandler interface {
	// Setup sets up action with manager in AddToMgr, i.e., watch, cache...
	Setup(controller.Controller, *mixin.ReconcilerMixin) error

	// OperateTargets do real operation to targets, and returns an error map to each target
	OperateTargets(context.Context, []*OpsCandidate, *appsv1alpha1.OperationJob) map[string]error

	// GetOpsProgress returns target's current opsStatus, e.g., progress, reason, message
	GetOpsProgress(context.Context, *OpsCandidate, *appsv1alpha1.OperationJob) (progress ActionProgress, err error)

	// ReleaseTargets releases the target from operation when failed, and returns an error map to each target
	ReleaseTargets(context.Context, []*OpsCandidate, *appsv1alpha1.OperationJob) map[string]error
}
