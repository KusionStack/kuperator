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

package ruleset

import (
	"context"
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

type MutatingHandler struct {
	client.Client
	*admission.Decoder
}

func NewMutatingHandler() *MutatingHandler {
	return &MutatingHandler{}
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.Operation != admissionv1.Update && req.Operation != admissionv1.Create {
		return admission.Allowed("")
	}
	rs := &appsv1alpha1.RuleSet{}
	if err := h.Decode(req, rs); err != nil {
		klog.Errorf("decode RuleSet validating request failed, %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	SetDefaultRuleSet(rs)
	marshalled, err := json.Marshal(rs)
	if err != nil {
		klog.Errorf("marshal RuleSet failed, %v", err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

func SetDefaultRuleSet(rs *appsv1alpha1.RuleSet) {
	for i := range rs.Spec.Rules {
		if rs.Spec.Rules[i].Webhook != nil {
			if rs.Spec.Rules[i].Webhook.ClientConfig.IntervalSeconds == nil {
				interval := appsv1alpha1.DefaultWebhookInterval
				rs.Spec.Rules[i].Webhook.ClientConfig.IntervalSeconds = &interval
			}
			if rs.Spec.Rules[i].Webhook.ClientConfig.TraceTimeoutSeconds == nil {
				timeout := appsv1alpha1.DefaultWebhookTimeout
				rs.Spec.Rules[i].Webhook.ClientConfig.TraceTimeoutSeconds = &timeout
			}
			if rs.Spec.Rules[i].Webhook.FailurePolicy == nil {
				failurePolicy := appsv1alpha1.Ignore
				rs.Spec.Rules[i].Webhook.FailurePolicy = &failurePolicy
			}
		}
	}
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
