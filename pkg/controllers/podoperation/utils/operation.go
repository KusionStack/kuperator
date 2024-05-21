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
	"context"
	"fmt"

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/podoperation/opscontrol"
	ctrlutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	operatingutils "kusionstack.io/operating/pkg/utils"
)

func MarkPodOperationFailed(instance *appsv1alpha1.PodOperation) {
	if instance.Status.Progress != appsv1alpha1.OperationProgressCompleted {
		now := ctrlutils.FormatTimeNow()
		instance.Status.Progress = appsv1alpha1.OperationProgressFailed
		instance.Status.EndTimestamp = &now
	}
}

func MapOpsStatusByPod(instance *appsv1alpha1.PodOperation) map[string]*appsv1alpha1.PodOpsStatus {
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

func GetCollaSetByPod(ctx context.Context, client client.Client, instance *appsv1alpha1.PodOperation, candidate *OpsCandidate) (*appsv1alpha1.CollaSet, error) {
	var collaSet appsv1alpha1.CollaSet
	ownedByCollaSet := false

	pod := candidate.Pod
	if pod == nil {
		if candidate.ReplaceNewPod != nil {
			pod = candidate.ReplaceNewPod
		} else {
			// pod is deleted by others or not exist, just ignore
			return nil, nil
		}
	}

	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind != "CollaSet" {
			continue
		}
		ownedByCollaSet = true
		err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: ownerRef.Name}, &collaSet)
		if err != nil {
			return nil, err
		}
	}

	if !ownedByCollaSet {
		return nil, fmt.Errorf("target %s/%s is not owned by collaSet", pod.Namespace, pod.Name)
	}

	return &collaSet, nil
}

func CancelOpsLifecycle(ctx context.Context, client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}
	labelUndo := fmt.Sprintf("%s/%s", appsv1alpha1.PodUndoOperationTypeLabelPrefix, adapter.GetID())
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[labelUndo] = string(adapter.GetType())
	return client.Update(ctx, pod)
}

func BeginRecreateLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if updated, err := podopslifecycle.Begin(client, adapter, pod); err != nil {
		return fmt.Errorf("fail to begin PodOpsLifecycle for %s %s/%s: %s", adapter.GetType(), pod.Namespace, pod.Name, err)
	} else if updated {
		if err := StatusUpToDateExpectation.ExpectUpdate(operatingutils.ObjectKeyString(pod), pod.ResourceVersion); err != nil {
			return err
		}
	}
	return nil
}

func FinishRecreateLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}
	if updated, err := podopslifecycle.Finish(client, adapter, pod); err != nil {
		return fmt.Errorf("failed to finish PodOpsLifecycle for %s %s/%s: %s", adapter.GetType(), pod.Namespace, pod.Name, err)
	} else if updated {
		// add an expectation for this pod update, before next reconciling
		if err := StatusUpToDateExpectation.ExpectUpdate(operatingutils.ObjectKeyString(pod), pod.ResourceVersion); err != nil {
			return err
		}
	}
	return nil
}
