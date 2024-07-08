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
	"fmt"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
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
	var obj, old appsv1alpha1.OperationJob
	var allErrors field.ErrorList
	if req.Operation == admissionv1.Delete {
		return admission.ValidationResponse(true, "")
	}

	if req.Operation == admissionv1.Update {
		err := h.Decoder.DecodeRaw(req.OldObject, &old)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	if err := h.Decoder.Decode(req, &obj); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	fldPath := field.NewPath("spec")
	allErrors = append(allErrors, h.validateOpsType(&obj, fldPath)...)
	allErrors = append(allErrors, h.validatePartition(&obj, &old, fldPath)...)
	allErrors = append(allErrors, h.validateTTLAndActiveDeadline(&obj, fldPath)...)
	allErrors = append(allErrors, h.validateOpsTarget(&obj, &old, fldPath.Child("targets"))...)
	if len(allErrors) > 0 {
		return admission.ValidationResponse(false, allErrors.ToAggregate().Error())
	}
	return admission.ValidationResponse(true, "")
}

func (h *ValidatingHandler) validateOpsType(instance *appsv1alpha1.OperationJob, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if instance.Spec.Action != appsv1alpha1.OpsActionRecreate &&
		instance.Spec.Action != appsv1alpha1.OpsActionReplace {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("action"), instance.Spec.Action,
			fmt.Sprintf("should be one of: %s", strings.Join([]string{
				string(appsv1alpha1.OpsActionRecreate),
				string(appsv1alpha1.OpsActionReplace),
			}, ","))))
	}
	return allErrors
}

func (h *ValidatingHandler) validateOpsTarget(instance, old *appsv1alpha1.OperationJob, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	if len(instance.Spec.Targets) == 0 {
		allErrors = append(allErrors, field.Invalid(fldPath, instance.Spec.Targets, "target can not be empty"))
		return allErrors
	}

	podSets := sets.String{}
	for podIdx, target := range instance.Spec.Targets {
		podFldPath := fldPath.Index(podIdx).Child("podName")

		if instance.Spec.Action == appsv1alpha1.OpsActionRecreate {
			cntSets := sets.String{}
			for ctnIdx, containerName := range target.Containers {
				containerFldPath := fldPath.Index(podIdx).Child("containerName").Index(ctnIdx)
				if cntSets.Has(containerName) {
					allErrors = append(allErrors, field.Invalid(containerFldPath, containerName, fmt.Sprintf("container named %s exists multiple times", containerName)))
				}
				cntSets.Insert(containerName)
			}
		} else if len(target.Containers) != 0 {
			allErrors = append(allErrors, field.Invalid(fldPath, target.PodName, fmt.Sprintf("containerNames should be empty for %s action", instance.Spec.Action)))
		}

		if podSets.Has(target.PodName) {
			allErrors = append(allErrors, field.Invalid(podFldPath, target.PodName, fmt.Sprintf("pod named %s exists multiple times", target.PodName)))
		}
		podSets.Insert(target.PodName)
	}

	if len(old.Spec.Targets) > 0 && !equality.Semantic.DeepEqual(instance.Spec.Targets, old.Spec.Targets) {
		allErrors = append(allErrors, field.Invalid(fldPath, instance.Spec.Targets, "spec.targets filed is immutable"))
	}

	return allErrors
}

func (h *ValidatingHandler) validatePartition(instance, old *appsv1alpha1.OperationJob, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	oldPartition := ptr.Deref(old.Spec.Partition, 0)
	curPartition := ptr.Deref(instance.Spec.Partition, 0)

	if curPartition < 0 {
		allErrors = append(allErrors, field.Invalid(fldPath, curPartition, "should not be negative"))
	} else if oldPartition > curPartition {
		allErrors = append(allErrors, field.Invalid(fldPath, curPartition, fmt.Sprintf("should not be decreased. (from %d to %d)", oldPartition, curPartition)))
	}
	return allErrors
}

func (h *ValidatingHandler) validateTTLAndActiveDeadline(instance *appsv1alpha1.OperationJob, fldPath *field.Path) field.ErrorList {
	var allErrors field.ErrorList
	activeDeadlineSeconds := instance.Spec.ActiveDeadlineSeconds
	ttlSecondsAfterFinished := instance.Spec.TTLSecondsAfterFinished
	if activeDeadlineSeconds != nil && *activeDeadlineSeconds <= 0 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("activeDeadlineSeconds"), activeDeadlineSeconds, "should be larger than 0"))
	}
	if ttlSecondsAfterFinished != nil && *ttlSecondsAfterFinished <= 0 {
		allErrors = append(allErrors, field.Invalid(fldPath.Child("TTLSecondsAfterFinished"), ttlSecondsAfterFinished, "should be larger than 0"))
	}
	return allErrors
}
