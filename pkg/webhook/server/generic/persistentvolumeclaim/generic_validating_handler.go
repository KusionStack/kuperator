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

package persistentvolumeclaim

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"kusionstack.io/operating/pkg/utils/mixin"
)

var _ inject.Client = &ValidatingHandler{}
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
	if req.Operation == admissionv1.Delete {
		pvc := &corev1.PersistentVolumeClaim{}
		if err := h.Decoder.DecodeRaw(req.OldObject, pvc); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to unmarshal old object: %s", err))
		}

		if pvc.Status.Phase == corev1.ClaimPending {
			isMounted, err := h.IsPvcMountedOnPod(ctx, pvc)
			if err != nil {
				return admission.Errored(http.StatusBadRequest, err)
			} else if isMounted {
				return admission.Denied(fmt.Sprintf("failed to validate, delete pending pvc is not allowed: %s", pvc.Name))
			}
		}
	}
	return admission.Allowed("")
}

func (h *ValidatingHandler) IsPvcMountedOnPod(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (bool, error) {
	podList := &corev1.PodList{}
	if err := h.Client.List(ctx, podList, &client.ListOptions{Namespace: pvc.Namespace}); err != nil {
		return false, fmt.Errorf("fail to list pod, %s %s", pvc.Name, pvc.Namespace)
	}

	for _, pod := range podList.Items {
		if pod.Spec.Volumes == nil {
			continue
		}
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName == "" {
				continue
			}
			if pvc.Name == volume.PersistentVolumeClaim.ClaimName {
				return true, nil
			}
		}
	}

	return false, nil
}

var _ inject.Client = &ValidatingHandler{}

func (h *ValidatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}
