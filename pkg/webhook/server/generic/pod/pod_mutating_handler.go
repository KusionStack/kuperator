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

const (
	mutatingName = "pod-mutating-webhook"
)

type MutatingHandler struct {
	client.Client
	*admission.Decoder
}

func NewMutatingHandler() *MutatingHandler {
	return &MutatingHandler{}
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.Kind.Kind != "Pod" {
		return admission.Patched("Invalid kind")
	}

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Patched("Not Create or Update, but " + string(req.Operation))
	}

	pod := &corev1.Pod{}
	err := h.Decode(req, pod)
	if err != nil {
		klog.Errorf("decode request failed, %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	var oldPod *corev1.Pod
	if req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		oldPod = &corev1.Pod{}
		if err = h.DecodeRaw(req.OldObject, oldPod); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("fail to unmarshal old object: %s", err))
		}
	}

	for _, webhook := range webhooks {
		// mutating on new pod
		if err = webhook.Mutating(ctx, h.Client, oldPod, pod, req.Operation); err != nil {
			klog.Errorf("failed to mutate pod, %v", err)
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	marshalled, err := json.Marshal(pod)
	if err != nil {
		klog.Errorf("marshal Pod failed, %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

var _ inject.Client = &MutatingHandler{}

func (h *MutatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

var _ admission.DecoderInjector = &MutatingHandler{}

func (h *MutatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
