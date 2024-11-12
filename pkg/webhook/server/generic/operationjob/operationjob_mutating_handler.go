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

package operationjob

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"k8s.io/utils/pointer"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"kusionstack.io/kuperator/pkg/utils/mixin"
)

var _ inject.Client = &MutatingHandler{}
var _ admission.DecoderInjector = &MutatingHandler{}

var (
	defaultTTL            int32 = 30 * 60
	defaultActiveDeadline int32 = 3 * 60 * 60
)

type MutatingHandler struct {
	*mixin.WebhookHandlerMixin
}

func NewMutatingHandler() *MutatingHandler {
	return &MutatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	opj := &appsv1alpha1.OperationJob{}
	if err := h.Decoder.DecodeRaw(req.OldObject, opj); err != nil {
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to unmarshal object: %s", err))
	}

	SetDefaultSpec(opj)
	marshalled, err := json.Marshal(opj)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

func SetDefaultSpec(opj *appsv1alpha1.OperationJob) {
	if opj.Spec.ActiveDeadlineSeconds == nil {
		opj.Spec.ActiveDeadlineSeconds = pointer.Int32(defaultActiveDeadline)
	}
	if opj.Spec.TTLSecondsAfterFinished == nil {
		opj.Spec.TTLSecondsAfterFinished = pointer.Int32(defaultTTL)
	}
}
