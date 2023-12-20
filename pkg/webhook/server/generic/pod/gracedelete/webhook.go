/*
Copyright 2023 The KusionStack Authors.

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

package gracedelete

import (
	"context"
	"fmt"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/poddeletion"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type GraceDelete struct {
}

func New() *GraceDelete {
	return &GraceDelete{}
}

func (gd *GraceDelete) Name() string {
	return "GraceDeleteWebhook"
}

func (gd *GraceDelete) Validating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	if operation != admissionv1.Delete {
		return nil
	}

	pod := &corev1.Pod{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: oldPod.Namespace, Name: oldPod.Name}, pod); err != nil {
		if !errors.IsNotFound(err) {
			klog.Error(err, "failed to find pod")
			return err
		}

		klog.Info("pod is deleted")
		return nil
	}
	if !utils.ControlledByKusionStack(pod) {
		return nil
	}

	// if Pod is not begin a deletion PodOpsLifecycle, trigger it
	if !podopslifecycle.IsDuringOps(poddeletion.OpsLifecycleAdapter, pod) {
		if _, err := podopslifecycle.Begin(c, poddeletion.OpsLifecycleAdapter, pod); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle to delete Pod %s: %s", pod.Name, err)
		}
	}

	// if Pod is allow to operate, delete it
	if _, allowed := podopslifecycle.AllowOps(poddeletion.OpsLifecycleAdapter, 0, pod); !allowed {
		var finalizers []string
		for _, f := range pod.Finalizers {
			if strings.HasPrefix(f, v1alpha1.PodOperationProtectionFinalizerPrefix) {
				finalizers = append(finalizers, f)
			}
		}
		return fmt.Errorf("podOpsLifecycle denied delete request, since related resources and finalizers have not been processed. Waiting for removing finalizers: %v", finalizers)
	}
	return nil
}

func (gd *GraceDelete) Mutating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	return nil
}
