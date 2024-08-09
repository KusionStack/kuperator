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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

type ActionProgress string

const (
	ActionProgressProcessing ActionProgress = "Processing"
	ActionProgressFailed     ActionProgress = "Failed"
	ActionProgressSucceeded  ActionProgress = "Succeeded"
)

type ActionHandler interface {
	// Init Initializes resources, i.e., watch resources related to action, inject cache index to runtime...
	Init(client.Client, controller.Controller, *runtime.Scheme, cache.Cache) error

	// OperateTarget do real operation to target
	OperateTarget(context.Context, logr.Logger, record.EventRecorder, client.Client, *OpsCandidate, *appsv1alpha1.OperationJob) error

	// GetOpsProgress returns target's current opsStatus, e.g., progress, reason, message
	GetOpsProgress(context.Context, logr.Logger, record.EventRecorder, client.Client, *OpsCandidate, *appsv1alpha1.OperationJob) (
		progress ActionProgress, reason string, message string, err error)

	// ReleaseTarget releases the target from operation when the operationJob is deleted
	ReleaseTarget(context.Context, logr.Logger, record.EventRecorder, client.Client, *OpsCandidate, *appsv1alpha1.OperationJob) error
}
