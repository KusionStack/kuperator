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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kusionstack.io/kuperator/pkg/controllers/operationjob/opscore"
)

const (
	ReasonUpdateObjectFailed = "UpdateObjectFailed"
	ReasonGetObjectFailed    = "GetObjectFailed"
)

func MapOpsStatusByPod(instance *appsv1alpha1.OperationJob) map[string]*appsv1alpha1.OpsStatus {
	opsStatusMap := make(map[string]*appsv1alpha1.OpsStatus)
	for i, opsStatus := range instance.Status.TargetDetails {
		if instance.Status.TargetDetails[i].ExtraInfo == nil {
			instance.Status.TargetDetails[i].ExtraInfo = make(map[string]string)
		}
		opsStatusMap[opsStatus.Name] = &instance.Status.TargetDetails[i]
	}
	return opsStatusMap
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

func SetOpsStatusError(candidate *opscore.OpsCandidate, reason string, message string) {
	if candidate == nil || candidate.OpsStatus == nil {
		return
	}
	if reason == "" && message == "" {
		candidate.OpsStatus.Error = nil
		return
	}
	candidate.OpsStatus.Error = &appsv1alpha1.ErrorReasonMessage{
		Reason:  reason,
		Message: message,
	}
}

func UpdatePodWithRetry(ctx context.Context, c client.Client, obj client.Object, updateFn func(*corev1.Pod)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, pod); err != nil {
			return err
		}
		updateFn(pod)
		return c.Update(ctx, pod)
	})
}

func ConvertErrMapToList(errMap map[string]error) []error {
	var errList []error
	for _, v := range errMap {
		errList = append(errList, v)
	}
	return errList
}

func ConvertSyncErrMap(errMap *sync.Map) map[string]error {
	ret := make(map[string]error)
	errMap.Range(func(key, value any) bool {
		if value == nil {
			ret[key.(string)] = nil
		} else {
			ret[key.(string)] = value.(error)
		}
		return true
	})
	return ret
}
