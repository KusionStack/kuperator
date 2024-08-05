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
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/utils"
)

func BeginOperateLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if updated, err := podopslifecycle.Begin(client, adapter, pod); err != nil {
		return fmt.Errorf("fail to begin PodOpsLifecycle for %s %s/%s: %s", adapter.GetType(), pod.Namespace, pod.Name, err)
	} else if updated {
		if err := StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(pod), pod.ResourceVersion); err != nil {
			return err
		}
	}
	return nil
}

func FinishOperateLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}
	if updated, err := podopslifecycle.Finish(client, adapter, pod); err != nil {
		return fmt.Errorf("failed to finish PodOpsLifecycle for %s %s/%s: %s", adapter.GetType(), pod.Namespace, pod.Name, err)
	} else if updated {
		// add an expectation for this pod update, before next reconciling
		if err := StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(pod), pod.ResourceVersion); err != nil {
			return err
		}
	}
	return nil
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
