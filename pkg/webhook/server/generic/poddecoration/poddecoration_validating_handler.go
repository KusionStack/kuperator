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
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
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
	if req.Operation != admissionv1.Update && req.Operation != admissionv1.Create {
		return admission.Allowed("")
	}
	pd := &appsv1alpha1.PodDecoration{}
	if err := h.Decoder.Decode(req, pd); err != nil {
		klog.Errorf("fail to decode PodDecoration, %v", err)
		return admission.Errored(http.StatusBadRequest, err)
	}
	if err := ValidatePodDecoration(pd); err != nil {
		return admission.Denied(err.Error())
	}
	return admission.Allowed("")
}

func ValidatePodDecoration(pd *appsv1alpha1.PodDecoration) error {
	for _, container := range pd.Spec.Template.PrimaryContainers {
		if container.TargetPolicy == appsv1alpha1.InjectByName && container.Name == nil {
			return fmt.Errorf("invalid primaryContainers.ByName, target name cannot be nil")
		}
	}
	return nil
}
