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

package pod

import (
	"context"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/pkg/webhook/server/generic/pod/opslifecycle"
	"kusionstack.io/kafed/pkg/webhook/server/generic/pod/ruleset"
)

var (
	webhooks []AdmissionWebhook
)

type AdmissionWebhook interface {
	Name() string
	Mutating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error
	Validating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error
}

func init() {
	webhooks = append(webhooks, opslifecycle.New())
	webhooks = append(webhooks, ruleset.New())
}
