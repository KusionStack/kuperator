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

package utils

import (
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	ctrlutils "kusionstack.io/operating/pkg/controllers/utils"
)

func MarkOperationJobFailed(instance *appsv1alpha1.OperationJob) {
	if instance.Status.Progress != appsv1alpha1.OperationProgressCompleted {
		now := ctrlutils.FormatTimeNow()
		instance.Status.Progress = appsv1alpha1.OperationProgressFailed
		instance.Status.EndTimestamp = &now
	}
}

func MapOpsStatusByPod(instance *appsv1alpha1.OperationJob) map[string]*appsv1alpha1.PodOpsStatus {
	opsStatusMap := make(map[string]*appsv1alpha1.PodOpsStatus)
	for i, opsStatus := range instance.Status.PodDetails {
		opsStatusMap[opsStatus.PodName] = &instance.Status.PodDetails[i]
	}
	return opsStatusMap
}

func ContainerExistsInPod(pod *corev1.Pod, containers []string) bool {
	if pod == nil {
		return false
	}
	cntSets := sets.String{}
	for i := range pod.Spec.Containers {
		cntSets.Insert(pod.Spec.Containers[i].Name)
	}
	for _, container := range containers {
		if !cntSets.Has(container) {
			return false
		}
	}
	return true
}

func ParsePhaseByCrrPhase(phase kruisev1alpha1.ContainerRecreateRequestPhase) appsv1alpha1.ContainerPhase {
	switch phase {
	case kruisev1alpha1.ContainerRecreateRequestPending:
		return appsv1alpha1.ContainerPhasePending
	case kruisev1alpha1.ContainerRecreateRequestRecreating:
		return appsv1alpha1.ContainerPhaseRecreating
	case kruisev1alpha1.ContainerRecreateRequestSucceeded:
		return appsv1alpha1.ContainerPhaseSucceed
	case kruisev1alpha1.ContainerRecreateRequestFailed:
		return appsv1alpha1.ContainerPhaseFailed
	case kruisev1alpha1.ContainerRecreateRequestCompleted:
		return appsv1alpha1.ContainerPhaseCompleted
	default:
		return appsv1alpha1.ContainerPhasePending
	}
}
