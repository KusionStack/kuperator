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
	"strconv"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"kusionstack.io/kube-api/apps/v1alpha1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/poddeletion"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/features"
	"kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/feature"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	updateGraceDeleteTimestampAnnoInterval = 5 * time.Second
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
	// GraceDeleteWebhook FeatureGate defaults to false
	// Add '--feature-gates=GraceDeleteWebhook=true' to container args, to enable gracedelete webhook
	if !feature.DefaultFeatureGate.Enabled(features.GraceDeleteWebhook) || operation != admissionv1.Delete || !utils.ControlledByKusionStack(oldPod) {
		return nil
	}

	// if has no service-ready ReadinessGate, skip gracedelete
	hasReadinessGate := false
	if oldPod.Spec.ReadinessGates != nil {
		for _, readinessGate := range oldPod.Spec.ReadinessGates {
			if readinessGate.ConditionType == v1alpha1.ReadinessGatePodServiceReady {
				hasReadinessGate = true
				break
			}
		}
	}
	if !hasReadinessGate {
		return nil
	}

	// if pod is allowed to delete
	if _, allowed := podopslifecycle.AllowOps(poddeletion.OpsLifecycleAdapter, 0, oldPod); allowed {
		return nil
	}

	// label pod to trigger poddeletion_controller reconcile
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newPod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Namespace: oldPod.Namespace, Name: oldPod.Name}, newPod)
		if err != nil {
			return err
		}
		if newPod.Labels == nil {
			newPod.Labels = map[string]string{}
		}
		if newPod.Annotations == nil {
			newPod.Annotations = map[string]string{}
		}

		// limit the AnnotationGraceDeleteTimestamp update frequency to updateGraceDeleteTimestampAnnoInterval,
		// to avoid update conflict caused by workload delete pod constantly
		if timestamp, ok := newPod.Annotations[appsv1alpha1.AnnotationGraceDeleteTimestamp]; ok {
			lastDeleteTimestamp, err := strconv.ParseInt(timestamp, 10, 64)
			if err == nil && time.Now().UnixNano()-lastDeleteTimestamp < int64(updateGraceDeleteTimestampAnnoInterval) {
				return nil
			}
		}

		if _, ok := newPod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; !ok {
			newPod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = strconv.FormatInt(time.Now().UnixNano(), 10)
		}
		newPod.Annotations[appsv1alpha1.AnnotationGraceDeleteTimestamp] = strconv.FormatInt(time.Now().UnixNano(), 10)

		return c.Update(ctx, newPod)
	})

	if err != nil {
		return err
	}

	var finalizers []string
	for _, f := range oldPod.Finalizers {
		if strings.HasPrefix(f, v1alpha1.PodOperationProtectionFinalizerPrefix) {
			finalizers = append(finalizers, f)
		}
	}

	if len(finalizers) == 0 {
		return fmt.Errorf("pod deletion process is underway and being managed by PodOpsLifecycle")
	}

	return fmt.Errorf("pod deletion process is underway and being managed by PodOpsLifecycle with finalizers: %v", finalizers)
}

func (gd *GraceDelete) Mutating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	return nil
}
