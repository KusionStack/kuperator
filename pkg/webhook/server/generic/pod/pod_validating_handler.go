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
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	commonutils "kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

var _ inject.Client = &ValidatingHandler{}
var _ inject.Logger = &ValidatingHandler{}
var _ admission.DecoderInjector = &ValidatingHandler{}

type ValidatingHandler struct {
	*mixin.WebhookHandlerMixin
}

func NewValidatingHandler() *ValidatingHandler {
	return &ValidatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	logger := h.Logger.WithValues(
		"op", req.Operation,
		"pod", commonutils.AdmissionRequestObjectKeyString(req),
	)

	pod := &corev1.Pod{}
	if req.Operation != admissionv1.Delete {
		if err := h.Decoder.Decode(req, pod); err != nil {
			s, _ := json.Marshal(req)
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to decode old object from request %s: %s", s, err))
		}
	}

	var oldPod *corev1.Pod
	if req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		oldPod = &corev1.Pod{}
		if err := h.Decoder.DecodeRaw(req.OldObject, oldPod); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to unmarshal old object: %s", err))
		}
	}

	for _, webhook := range Webhooks {
		if err := webhook.Validating(ctx, h.Client, oldPod, pod, req.Operation); err != nil {
			logger.Error(err, "failed to validate pod")
			return admission.Denied(fmt.Sprintf("failed to validate %s, %v", webhook.Name(), err))
		}
	}

	return admission.Allowed("")
}
