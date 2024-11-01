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

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/kubernetes/pkg/apis/core"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8scorev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	kuperatorv1alpha1 "kusionstack.io/kuperator/apis/apps/v1alpha1"
	commonutils "kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
	"kusionstack.io/kuperator/pkg/webhook/server/generic/utils"
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
	kuperatorv1alpha1.SetDefaultPodSpec(cls)

	var oldCls *appsv1alpha1.CollaSet
	if req.Operation == admissionv1.Update || req.Operation == admissionv1.Delete {
		oldCls = &appsv1alpha1.CollaSet{}
		if err := h.Decoder.DecodeRaw(req.OldObject, oldCls); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to unmarshal old object: %s", err))
		}
		kuperatorv1alpha1.SetDefaultPodSpec(oldCls)
	}

	if err := h.validate(cls, oldCls); err != nil {
		return admission.Errored(http.StatusUnprocessableEntity, err)
	}

	return admission.Allowed("")
}

func (h *ValidatingHandler) validate(cls, oldCls *appsv1alpha1.CollaSet) error {
	var allErrs field.ErrorList
	fSpec := field.NewPath("spec")

	if fieldErr := h.validateReplicas(cls, fSpec); fieldErr != nil {
		allErrs = append(allErrs, fieldErr)
	}
	allErrs = append(allErrs, h.validatePodTemplateSpec(cls, fSpec)...)
	allErrs = append(allErrs, h.validateSelector(cls, fSpec)...)
	allErrs = append(allErrs, h.validateScaleStrategy(cls, oldCls, fSpec)...)
	allErrs = append(allErrs, h.validateUpdateStrategy(cls, fSpec)...)

	return allErrs.ToAggregate()
}

func (h *ValidatingHandler) validateScaleStrategy(cls, oldCls *appsv1alpha1.CollaSet, fSpec *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if cls.Spec.ScaleStrategy.OperationDelaySeconds != nil && *cls.Spec.ScaleStrategy.OperationDelaySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fSpec.Child("scaleStrategy", "operationDelaySeconds"),
			*cls.Spec.ScaleStrategy.OperationDelaySeconds, "operationDelaySeconds should not be smaller than 0"))
	}

	if oldCls != nil && oldCls.Spec.ScaleStrategy.Context != cls.Spec.ScaleStrategy.Context {
		allErrs = append(allErrs, field.Forbidden(fSpec.Child("scaleStrategy", "context"), "scaleStrategy.context is not allowed to be changed"))
	}

	allErrs = append(allErrs, h.validateScaleStrategyPodList(cls, fSpec)...)

	return allErrs
}

func (h *ValidatingHandler) validateScaleStrategyPodList(cls *appsv1alpha1.CollaSet, fSpec *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	excludePodNames := sets.NewString(cls.Spec.ScaleStrategy.PodToExclude...)
	includePodNames := sets.NewString(cls.Spec.ScaleStrategy.PodToInclude...)
	deletePodNames := sets.NewString(cls.Spec.ScaleStrategy.PodToDelete...)
	isBtExInclude := excludePodNames.Intersection(includePodNames)
	isBtDeInclude := deletePodNames.Intersection(includePodNames)
	isBtDeExclude := deletePodNames.Intersection(excludePodNames)
	if len(isBtExInclude) > 0 {
		allErrs = append(allErrs, field.Forbidden(fSpec.Child("scaleStrategy", "podToExclude[podToInclude]"), fmt.Sprintf("scaleStrategy.podToExclude[podToInclude] should not have intersection %v", isBtExInclude.List())))
	}
	if len(isBtDeInclude) > 0 {
		allErrs = append(allErrs, field.Forbidden(fSpec.Child("scaleStrategy", "podToDelete[podToInclude]"), fmt.Sprintf("scaleStrategy.podToDelete[podToInclude] should not have intersection %v", isBtDeInclude.List())))
	}
	if len(isBtDeExclude) > 0 {
		allErrs = append(allErrs, field.Forbidden(fSpec.Child("scaleStrategy", "podToDelete[podToExclude]"), fmt.Sprintf("scaleStrategy.podToDelete[podToExclude] should not have intersection %v", isBtDeExclude.List())))
	}
	return allErrs
}

func (h *ValidatingHandler) validateUpdateStrategy(cls *appsv1alpha1.CollaSet, fSpec *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType,
		appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType,
		appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType,
		appsv1alpha1.CollaSetReplacePodUpdateStrategyType:
	default:
		allErrs = append(allErrs, field.NotSupported(fSpec.Child("updateStrategy", "podUpdatePolicy"),
			cls.Spec.UpdateStrategy.PodUpdatePolicy, []string{string(appsv1alpha1.CollaSetRecreatePodUpdateStrategyType),
				string(appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType),
				string(appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType),
				string(appsv1alpha1.CollaSetReplacePodUpdateStrategyType)}))
	}

	if cls.Spec.UpdateStrategy.RollingUpdate != nil && cls.Spec.UpdateStrategy.RollingUpdate.ByPartition != nil &&
		cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition != nil &&
		*cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition < 0 {
		allErrs = append(allErrs, field.Invalid(fSpec.Child("updateStrategy", "rollingUpdate",
			"byPartition", "partition"), *cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition,
			"partition should not be smaller than 0"))
	}

	if cls.Spec.UpdateStrategy.OperationDelaySeconds != nil && *cls.Spec.UpdateStrategy.OperationDelaySeconds < 0 {
		allErrs = append(allErrs, field.Invalid(fSpec.Child("updateStrategy", "operationDelaySeconds"),
			*cls.Spec.UpdateStrategy.OperationDelaySeconds, "operationDelaySeconds should not be smaller than 0"))
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

	for _, pvc := range cls.Spec.VolumeClaimTemplates {
		podTemplateSpec.Spec.Volumes = append(podTemplateSpec.Spec.Volumes, core.Volume{
			Name: pvc.Name,
			VolumeSource: core.VolumeSource{
				PersistentVolumeClaim: &core.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
					ReadOnly:  false,
				},
			},
		})
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
