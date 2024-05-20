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

package opscontrol

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type ActionOperator interface {
	ListTargets() ([]*OpsCandidate, error)
	OperateTarget(*OpsCandidate) error
	FulfilPodOpsStatus(*OpsCandidate) error
	ReleaseTarget(*OpsCandidate) error
}

type OperateInfo struct {
	Context  context.Context
	Logger   logr.Logger
	Client   client.Client
	Recorder record.EventRecorder
	*appsv1alpha1.OperationJob
}
