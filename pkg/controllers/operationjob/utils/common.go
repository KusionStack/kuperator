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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscore"
	ctrlutils "kusionstack.io/operating/pkg/controllers/utils"
)

func MarkOperationJobFailed(instance *appsv1alpha1.OperationJob) {
	if instance.Status.Progress != appsv1alpha1.OperationProgressSucceeded {
		now := ctrlutils.FormatTimeNow()
		instance.Status.Progress = appsv1alpha1.OperationProgressFailed
		instance.Status.EndTimestamp = &now
	}
}

func MapOpsStatusByPod(instance *appsv1alpha1.OperationJob) map[string]*appsv1alpha1.OpsStatus {
	opsStatusMap := make(map[string]*appsv1alpha1.OpsStatus)
	for i, opsStatus := range instance.Status.TargetDetails {
		opsStatusMap[opsStatus.Name] = &instance.Status.TargetDetails[i]
	}
	return opsStatusMap
}

func GetCollaSetByPod(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, candidate *OpsCandidate) (*appsv1alpha1.CollaSet, error) {
	var collaSet appsv1alpha1.CollaSet
	ownedByCollaSet := false

	pod := candidate.Pod
	if pod == nil {
		// replace completed, just ignore
		return nil, nil
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

func EnqueueOperationJobFromPod(c client.Client, pod *corev1.Pod, q *workqueue.RateLimitingInterface, fn func(client.Client, *corev1.Pod) (sets.String, bool)) {
	if ojNames, owned := fn(c, pod); owned {
		for ojName := range ojNames {
			(*q).Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pod.Namespace,
					Name:      ojName,
				},
			})
		}
	}
}

func IsJobFinished(instance *appsv1alpha1.OperationJob) bool {
	return instance.Status.Progress == appsv1alpha1.OperationProgressSucceeded ||
		instance.Status.Progress == appsv1alpha1.OperationProgressFailed
}
