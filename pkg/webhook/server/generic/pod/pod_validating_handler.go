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
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type ValidatingHandler struct {
	client.Client
	*admission.Decoder
}

func NewValidatingHandler() *ValidatingHandler {
	return &ValidatingHandler{}
}

func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.Operation == admissionv1.Delete {
		return admission.Allowed("pod is allowed by opslifecycle")
	}

	pod := &corev1.Pod{}
	if err := h.Decode(req, pod); err != nil {
		s, _ := json.Marshal(req)
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("fail to decode old object from request %s: %s", s, err))
	}

	var oldPod *corev1.Pod
	if req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		oldPod = &corev1.Pod{}
		if err := h.DecodeRaw(req.OldObject, oldPod); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("fail to unmarshal old object: %s", err))
		}
	}

	for _, webhook := range webhooks {
		if err := webhook.Validating(ctx, h.Client, oldPod, pod, req.Operation); err != nil {
			klog.Errorf("failed to validate pod, %v", err)
			return admission.Denied(fmt.Sprintf("fail to validate %s, %v", webhook.Name(), err))
		}
	}

	return admission.Allowed("")
}

var _ inject.Client = &ValidatingHandler{}

func (h *ValidatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

var _ admission.DecoderInjector = &ValidatingHandler{}

func (h *ValidatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
