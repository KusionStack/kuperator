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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	podopslifecycleutil "kusionstack.io/kuperator/pkg/controllers/podopslifecycle"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/utils"
)

func BeginOperateLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) (bool, error) {
	if pod == nil {
		return false, nil
	}

	// not start to operate until other lifecycles finished
	if num, err := podopslifecycleutil.NumOfLifecycleOnPod(pod); err != nil {
		return false, err
	} else if num > 0 {
		return false, nil
	}

	if updated, err := podopslifecycle.Begin(client, adapter, pod); err != nil {
		return false, fmt.Errorf("fail to begin PodOpsLifecycle for %s %s/%s: %w", adapter.GetType(), pod.Namespace, pod.Name, err)
	} else if updated {
		if err := StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(pod), pod.ResourceVersion); err != nil {
			return true, err
		}
	}
	return true, nil
}

func FinishOperateLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}
	if updated, err := podopslifecycle.Finish(client, adapter, pod); err != nil {
		return fmt.Errorf("failed to finish PodOpsLifecycle for %s %s/%s: %w", adapter.GetType(), pod.Namespace, pod.Name, err)
	} else if updated {
		// add an expectation for this pod update, before next reconciling
		if err := StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(pod), pod.ResourceVersion); err != nil {
			return err
		}
	}
	return nil
}

func CancelOpsLifecycle(client client.Client, adapter podopslifecycle.LifecycleAdapter, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}

	// only cancel when lifecycle exist on pod
	if exist, err := podopslifecycleutil.IsLifecycleOnPod(adapter.GetID(), pod); err != nil {
		return fmt.Errorf("fail to check %s PodOpsLifecycle on Pod %s/%s: %w", adapter.GetID(), pod.Namespace, pod.Name, err)
	} else if !exist {
		return nil
	}

	return podopslifecycle.Undo(client, adapter, pod)
}
