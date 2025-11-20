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

package resourcecontext

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	podcontext "kusionstack.io/kuperator/pkg/controllers/xcollaset/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

var (
	_ inject.Client             = &ValidatingHandler{}
	_ admission.DecoderInjector = &ValidatingHandler{}
)

type ValidatingHandler struct {
	*mixin.WebhookHandlerMixin
}

func NewValidatingHandler() *ValidatingHandler {
	return &ValidatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	var newObj appsv1alpha1.ResourceContext
	if req.Operation == admissionv1.Delete {
		return admission.ValidationResponse(true, "")
	}
	if err := h.Decoder.DecodeRaw(req.Object, &newObj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	if allErrors := h.validateOwner(&newObj); len(allErrors) > 0 {
		return admission.Errored(http.StatusBadRequest, allErrors.ToAggregate())
	}
	return admission.Allowed("")
}

func (h *ValidatingHandler) validateOwner(instance *appsv1alpha1.ResourceContext) field.ErrorList {
	var allErrors field.ErrorList
	dataFiledPath := field.NewPath("spec").Child("data")
	for _, contextDetail := range instance.Spec.Contexts {
		if contextDetail.Data == nil {
			continue
		}
		if owner, exist := contextDetail.Data[podcontext.OwnerContextKey]; exist && owner != instance.Name {
			allErrors = append(allErrors, field.Invalid(dataFiledPath, owner, fmt.Sprintf("owner name %s is not match for resource context %s exists multiple times", owner, instance.Name)))
		}
	}
	return allErrors
}
