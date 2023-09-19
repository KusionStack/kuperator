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

package resourceconsist

import (
	"context"
	"encoding/json"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/webhook/server/generic/pod/resourceconsist/webhookAdapters"
)

var PodResourceConsistWebhooks []*PodResourceConsistWebhook

type PodResourceConsistWebhook struct {
	webhookAdapters.WebhookAdapter
}

func NewPodResourceConsistWebhook(adapter webhookAdapters.WebhookAdapter) *PodResourceConsistWebhook {
	return &PodResourceConsistWebhook{
		adapter,
	}
}

func (r *PodResourceConsistWebhook) Name() string {
	return "PodResourceConsistWebhook"
}

func (r *PodResourceConsistWebhook) Mutating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	if newPod == nil {
		return nil
	}

	// only concern pods new created
	if operation != admissionv1.Create {
		return nil
	}

	employers, err := r.WebhookAdapter.GetEmployersByEmployee(ctx, newPod, c)
	if err != nil {
		return err
	}

	availableExpectedFlzs := v1alpha1.PodAvailableConditions{
		ExpectedFinalizers: map[string]string{},
	}
	for _, employer := range employers {
		expectedFlzKey := GenerateLifecycleFinalizerKey(employer)
		expectedFlz := GenerateLifecycleFinalizer(employer.GetName())
		availableExpectedFlzs.ExpectedFinalizers[expectedFlzKey] = expectedFlz
	}
	annoAvailableCondition, err := json.Marshal(availableExpectedFlzs)
	if err != nil {
		return err
	}
	if newPod.Annotations == nil {
		newPod.Annotations = make(map[string]string)
	}
	newPod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] = string(annoAvailableCondition)

	return nil
}

func (r *PodResourceConsistWebhook) Validating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	return nil
}

func init() {
	for _, webhookAdapter := range webhookAdapters.WebhookAdapters {
		PodResourceConsistWebhooks = append(PodResourceConsistWebhooks, NewPodResourceConsistWebhook(webhookAdapter))
	}
}
