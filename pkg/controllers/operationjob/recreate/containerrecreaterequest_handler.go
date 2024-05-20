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
	"fmt"

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
)

type ContainerRecreateRequestHandler struct {
}

func (h *ContainerRecreateRequestHandler) DoRestartContainers(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob,
	candidate *opscontrol.OpsCandidate, containers []string) error {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, candidate.PodName)

	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if errors.IsNotFound(err) {
		var crrContainers []kruisev1alpha1.ContainerRecreateRequestContainer
		for _, container := range containers {
			crrContainer := kruisev1alpha1.ContainerRecreateRequestContainer{
				Name: container,
			}
			crrContainers = append(crrContainers, crrContainer)
		}

		crr = &kruisev1alpha1.ContainerRecreateRequest{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: instance.Namespace,
				Name:      fmt.Sprintf("%s-%s", instance.Name, candidate.PodName),
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(instance, appsv1alpha1.GroupVersion.WithKind("OperationJob")),
				},
			},
			Spec: kruisev1alpha1.ContainerRecreateRequestSpec{
				PodName:    candidate.PodName,
				Containers: crrContainers,
			},
		}

		if err = client.Create(ctx, crr); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

func (h *ContainerRecreateRequestHandler) IsRestartFinished(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob, candidate *opscontrol.OpsCandidate) bool {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, candidate.PodName)

	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if err != nil {
		return false
	}
	return crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestCompleted
}

func (h *ContainerRecreateRequestHandler) GetContainerOpsPhase(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob,
	candidate *opscontrol.OpsCandidate, container string) appsv1alpha1.ContainerPhase {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, candidate.PodName)

	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if err != nil {
		return appsv1alpha1.ContainerPhasePending
	}

	for _, state := range crr.Status.ContainerRecreateStates {
		if state.Name == container {
			return ojutils.ParsePhaseByCrrPhase(crr.Status.Phase)
		}
	}
	return appsv1alpha1.ContainerPhasePending
}

func (h *ContainerRecreateRequestHandler) FulfilExtraInfo(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob,
	candidate *opscontrol.OpsCandidate, extraInfo *map[appsv1alpha1.ExtraInfoKey]string) {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, candidate.PodName)

	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if err != nil {
		return
	}
	(*extraInfo)[appsv1alpha1.CRRKey] = crr.Name
}

func (h *ContainerRecreateRequestHandler) ReleasePod(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob,
	candidate *opscontrol.OpsCandidate) error {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, candidate.PodName)

	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	return client.Delete(ctx, crr)
}
