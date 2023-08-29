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

package collaset

import (
	"context"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	k8scorev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	commonutils "kusionstack.io/kafed/pkg/utils"
	"kusionstack.io/kafed/pkg/utils/mixin"
	"kusionstack.io/kafed/pkg/webhook/server/generic/utils"
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
	if req.Operation == admissionv1.Delete {
		return admission.Allowed("")
	}

	logger := h.Logger.WithValues(
		"op", req.Operation,
		"collaset", commonutils.AdmissionRequestObjectKeyString(req),
	)

	cls := &appsv1alpha1.CollaSet{}
	if err := h.Decoder.Decode(req, cls); err != nil {
		logger.Error(err, "failed to decode collaset")
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := h.validate(cls); err != nil {
		return admission.Errored(http.StatusUnprocessableEntity, err)
	}

	return admission.Allowed("")
}

func (h *ValidatingHandler) validate(cls *appsv1alpha1.CollaSet) error {
	var allErrs field.ErrorList
	fSpec := field.NewPath("spec")

	if fieldErr := h.validateReplicas(cls, fSpec); fieldErr != nil {
		allErrs = append(allErrs, fieldErr)
	}
	allErrs = append(allErrs, h.validatePodTemplateSpec(cls, fSpec)...)
	allErrs = append(allErrs, h.validateSelector(cls, fSpec)...)
	allErrs = append(allErrs, h.validateUpdateStrategy(cls, fSpec)...)

	return allErrs.ToAggregate()
}

func (h *ValidatingHandler) validateUpdateStrategy(cls *appsv1alpha1.CollaSet, fSpec *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType,
		appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType,
		appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType:
	default:
		allErrs = append(allErrs, field.Invalid(fSpec.Child("updateStrategy", "podUpdatePolicy"),
			cls.Spec.UpdateStrategy.PodUpdatePolicy, fmt.Sprintf("podUpdatePolicy should be one of %v",
				[]appsv1alpha1.PodUpdateStrategyType{appsv1alpha1.CollaSetRecreatePodUpdateStrategyType,
					appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType,
					appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType})))
	}

	if cls.Spec.UpdateStrategy.RollingUpdate != nil && cls.Spec.UpdateStrategy.RollingUpdate.ByPartition != nil &&
		cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition != nil &&
		*cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition < 0 {
		allErrs = append(allErrs, field.Invalid(fSpec.Child("updateStrategy", "rollingUpdate",
			"byPartition", "partition"), *cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition,
			"partition should not be smaller than 0"))
	}

	return allErrs
}

func (h *ValidatingHandler) validateReplicas(cls *appsv1alpha1.CollaSet, fSpec *field.Path) *field.Error {
	if cls.Spec.Replicas != nil && *cls.Spec.Replicas < 0 {
		return field.Invalid(fSpec.Child("replicas"), *cls.Spec.Replicas,
			"replicas should not be smaller than 0")
	}

	return nil
}

func (h *ValidatingHandler) validatePodTemplateSpec(cls *appsv1alpha1.CollaSet, fSpec *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	podTemplateSpec := &core.PodTemplateSpec{}
	if err := k8scorev1.Convert_v1_PodTemplateSpec_To_core_PodTemplateSpec(cls.Spec.Template.DeepCopy(), podTemplateSpec, nil); err != nil {
		return append(allErrs, field.Invalid(fSpec.Child("template"), cls.Spec.Template, fmt.Sprintf("fail to convert to core PodTemplateSpec: %s", err)))
	}

	return corevalidation.ValidatePodTemplateSpec(podTemplateSpec, fSpec, utils.PodValidationOptions)
}

func (h *ValidatingHandler) validateSelector(cls *appsv1alpha1.CollaSet, fSpec *field.Path) field.ErrorList {
	var allError field.ErrorList

	if cls.Spec.Selector == nil {
		return append(allError, field.Invalid(fSpec.Child("selector"), nil, "selector is required"))
	} else {
		errList := metav1validation.ValidateLabelSelector(cls.Spec.Selector, fSpec.Child("selector"))
		if len(errList) > 0 {
			return errList
		}
	}

	if cls.Spec.Template.Labels == nil {
		return append(allError, field.Invalid(fSpec.Child("template", "metadata", "labels"), nil, "labels is required"))
	}

	selector, err := metav1.LabelSelectorAsSelector(cls.Spec.Selector)
	if err != nil {
		return append(allError, field.Invalid(fSpec.Child("selector"), nil, "selector is malformed"))
	}

	if !selector.Matches(labels.Set(cls.Spec.Template.Labels)) {
		return append(allError, field.Invalid(fSpec.Child("template", "metadata", "labels"), nil, "selector does not match labels in pod template"))
	}

	return allError
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
