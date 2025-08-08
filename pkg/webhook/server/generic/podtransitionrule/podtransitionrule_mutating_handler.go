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

package podtransitionrule

import (
	"context"
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	commonutils "kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

var _ inject.Client = &MutatingHandler{}
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
	if req.Operation != admissionv1.Update && req.Operation != admissionv1.Create {
		return admission.Allowed("")
	}

	logger := h.Logger.WithValues(
		"op", req.Operation,
		"podtransitionrule", commonutils.AdmissionRequestObjectKeyString(req),
	)

	rs := &appsv1alpha1.PodTransitionRule{}
	if err := h.Decoder.Decode(req, rs); err != nil {
		logger.Error(err, "failed to decode podtransitionrule")
		return admission.Errored(http.StatusBadRequest, err)
	}
	SetDefaultPodTransitionRule(rs)
	marshaled, err := json.Marshal(rs)
	if err != nil {
		logger.Error(err, "failed to marshal podtransitionrule json")
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshaled)
}

func SetDefaultPodTransitionRule(rs *appsv1alpha1.PodTransitionRule) {
	for i := range rs.Spec.Rules {
		if rs.Spec.Rules[i].Webhook != nil {
			if rs.Spec.Rules[i].Webhook.ClientConfig.Poll != nil {
				if rs.Spec.Rules[i].Webhook.ClientConfig.Poll.IntervalSeconds == nil {
					interval := appsv1alpha1.DefaultWebhookInterval
					rs.Spec.Rules[i].Webhook.ClientConfig.Poll.IntervalSeconds = &interval
				}
				if rs.Spec.Rules[i].Webhook.ClientConfig.Poll.TimeoutSeconds == nil {
					timeout := appsv1alpha1.DefaultWebhookTimeout
					rs.Spec.Rules[i].Webhook.ClientConfig.Poll.TimeoutSeconds = &timeout
				}
			}
			if rs.Spec.Rules[i].Webhook.FailurePolicy == nil {
				failurePolicy := appsv1alpha1.Ignore
				rs.Spec.Rules[i].Webhook.FailurePolicy = &failurePolicy
			}
		}
	}
}
