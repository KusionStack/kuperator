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

package poddecoration

import (
	"context"
	"encoding/json"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

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
	pd := &appsv1alpha1.PodDecoration{}
	if err := h.Decoder.Decode(req, pd); err != nil {
		klog.Errorf("fail to decode PodDecoration, %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	SetDefaultPodDecoration(pd)
	marshaled, err := json.Marshal(pd)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshaled)
}

func SetDefaultPodDecoration(pd *appsv1alpha1.PodDecoration) {
	if pd.Spec.Weight == nil {
		var int32Zero int32
		pd.Spec.Weight = &int32Zero
	}
	for i := range pd.Spec.Template.Metadata {
		if pd.Spec.Template.Metadata[i].PatchPolicy == "" {
			pd.Spec.Template.Metadata[i].PatchPolicy = appsv1alpha1.RetainMetadata
		}
	}
	for i := range pd.Spec.Template.Containers {
		if pd.Spec.Template.Containers[i].InjectPolicy == "" {
			pd.Spec.Template.Containers[i].InjectPolicy = appsv1alpha1.BeforePrimaryContainer
		}
	}
	for i := range pd.Spec.Template.PrimaryContainers {
		if pd.Spec.Template.PrimaryContainers[i].TargetPolicy == "" {
			pd.Spec.Template.PrimaryContainers[i].TargetPolicy = appsv1alpha1.InjectAllContainers
		}
	}
	if pd.Spec.HistoryLimit == 0 {
		pd.Spec.HistoryLimit = 20
	}
}
