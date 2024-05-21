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

package recreate

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/podoperation/opscontrol"
)

type RestartHandler interface {
	DoRestartContainers(context.Context, client.Client, *appsv1alpha1.PodOperation, *opscontrol.OpsCandidate, []string) error
	IsRestartFinished(context.Context, client.Client, *appsv1alpha1.PodOperation, *opscontrol.OpsCandidate) bool
	GetContainerOpsPhase(context.Context, client.Client, *appsv1alpha1.PodOperation, *opscontrol.OpsCandidate, string) appsv1alpha1.ContainerPhase
	FulfilExtraInfo(context.Context, client.Client, *appsv1alpha1.PodOperation, *opscontrol.OpsCandidate, *map[appsv1alpha1.ExtraInfoKey]string)
	ReleasePod(context.Context, client.Client, *appsv1alpha1.PodOperation, *opscontrol.OpsCandidate) error
}

var registry map[string]RestartHandler

func RegisterRecreateHandler(methodName string, handler RestartHandler) {
	if registry == nil {
		registry = make(map[string]RestartHandler)
	}
	registry[methodName] = handler
}

func GetRecreateHandler(methodName string) RestartHandler {
	if _, exist := registry[methodName]; exist {
		return registry[methodName]
	}
	return nil
}
