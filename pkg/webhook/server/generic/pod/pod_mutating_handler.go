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

	commonutils "kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

const (
	mutatingName = "pod-mutating-webhook"
)

var _ inject.Client = &MutatingHandler{}
var _ inject.Logger = &MutatingHandler{}
var _ admission.DecoderInjector = &MutatingHandler{}

type MutatingHandler struct {
	*mixin.WebhookHandlerMixin
}

func NewMutatingHandler() *MutatingHandler {
	return &MutatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.Kind.Kind != "Pod" {
		return admission.Patched("invalid kind")
	}

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Patched("not Create or Update, but " + string(req.Operation))
	}

	logger := h.Logger.WithValues(
		"op", req.Operation,
		"pod", commonutils.AdmissionRequestObjectKeyString(req),
	)

	pod := &corev1.Pod{}
	err := h.Decoder.Decode(req, pod)
	if err != nil {
		logger.Error(err, "failed to decode admission request")
		return admission.Errored(http.StatusBadRequest, err)
	}
	var oldPod *corev1.Pod
	if req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		oldPod = &corev1.Pod{}
		if err = h.Decoder.DecodeRaw(req.OldObject, oldPod); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("fail to unmarshal old object: %s", err))
		}
	}

	for _, webhook := range webhooks {
		// mutating on new pod
		if err = webhook.Mutating(ctx, h.Client, oldPod, pod, req.Operation); err != nil {
			logger.Error(err, "failed to mutate pod")
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	marshalled, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "failed to marshal pod json")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}
