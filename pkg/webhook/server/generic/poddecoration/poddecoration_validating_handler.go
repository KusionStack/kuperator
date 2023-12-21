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
	"net/http"
	"path"
	"path/filepath"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/apis/core"
	k8scorev1 "k8s.io/kubernetes/pkg/apis/core/v1"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
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

var (
	defaultValidationOptions = corevalidation.PodValidationOptions{
		AllowDownwardAPIHugePages:       true,
		AllowInvalidPodDeletionCost:     true,
		AllowIndivisibleHugePagesValues: true,
		AllowWindowsHostProcessField:    true,
		AllowExpandedDNSConfig:          true,
	}
)

func ValidatePodDecoration(pd *appsv1alpha1.PodDecoration) error {
	allErrs := field.ErrorList{}
	specPath := field.NewPath("spec")
	allErrs = append(allErrs, ValidateTemplate(&pd.Spec.Template, specPath.Child("template"))...)
	return allErrs.ToAggregate()
}

func ValidateTemplate(template *appsv1alpha1.PodDecorationPodTemplate, fldPath *field.Path) (allErrs field.ErrorList) {
	allErrs = append(allErrs, ValidatePrimaryContainers(template.PrimaryContainers, fldPath.Child("primaryContainers"))...)
	allErrs = append(allErrs, ValidatePodDecorationPodTemplateMeta(template.Metadata, fldPath.Child("metadata"))...)
	allErrs = append(allErrs, ValidateContainers(template.InitContainers, fldPath.Child("initContainers"))...)
	allErrs = append(allErrs, ValidateVolumes(template.Volumes, fldPath.Child("volumes"))...)
	allErrs = append(allErrs, ValidateTolerations(template.Tolerations, fldPath.Child("tolerations"))...)
	return
}

func ValidateTolerations(tolerations []corev1.Toleration, fldPath *field.Path) (allErrs field.ErrorList) {
	var coreTolerations []core.Toleration
	for i := range tolerations {
		idxPath := fldPath.Index(i)
		coreToleration := &core.Toleration{}
		if err := k8scorev1.Convert_v1_Toleration_To_core_Toleration(&tolerations[i], coreToleration, nil); err != nil {
			allErrs = append(allErrs, field.InternalError(idxPath, err))
		}
		coreTolerations = append(coreTolerations, *coreToleration)
	}

	allErrs = append(allErrs, corevalidation.ValidateTolerations(coreTolerations, fldPath)...)
	return
}

func ValidateVolumes(volumes []corev1.Volume, fldPath *field.Path) (allErrs field.ErrorList) {
	var coreVolumes []core.Volume
	for i := range volumes {
		idxPath := fldPath.Index(i)
		coreVolume := &core.Volume{}
		if err := k8scorev1.Convert_v1_Volume_To_core_Volume(&volumes[i], coreVolume, nil); err != nil {
			allErrs = append(allErrs, field.InternalError(idxPath, err))
		}
		coreVolumes = append(coreVolumes, *coreVolume)
	}
	_, errs := corevalidation.ValidateVolumes(coreVolumes, nil, fldPath, defaultValidationOptions)
	allErrs = append(allErrs, errs...)
	return
}

func ValidateContainers(containers []*corev1.Container, fldPath *field.Path) (allErrs field.ErrorList) {
	for i, c := range containers {
		coreContainer := &core.Container{}
		idxPath := fldPath.Index(i)
		if err := k8scorev1.Convert_v1_Container_To_core_Container(c, coreContainer, nil); err != nil {
			allErrs = append(allErrs, field.InternalError(idxPath, err))
		}
		// TODO: validate containers
	}
	return
}

func ValidatePodDecorationPodTemplateMeta(meta []*appsv1alpha1.PodDecorationPodTemplateMeta, fldPath *field.Path) (allErrs field.ErrorList) {
	for i, m := range meta {
		idxPath := fldPath.Index(i)
		if m.PatchPolicy == appsv1alpha1.MergePatchJsonMetadata && m.Labels != nil {
			allErrs = append(allErrs, field.Invalid(idxPath.Child("labels"), m.Labels, "patchPolicy MergePatchJson is only effective for annotations"))
		}
		allErrs = append(allErrs, corevalidation.ValidateAnnotations(m.Annotations, idxPath.Child("annotations"))...)
	}
	return
}

func ValidatePrimaryContainers(containers []*appsv1alpha1.PrimaryContainerPatch, fldPath *field.Path) (allErrs field.ErrorList) {
	for idx, container := range containers {
		allErrs = append(allErrs, ValidatePrimaryContainer(container, fldPath.Index(idx))...)
	}
	return
}

func ValidatePrimaryContainer(container *appsv1alpha1.PrimaryContainerPatch, fldPath *field.Path) (allErrs field.ErrorList) {
	if container.TargetPolicy == appsv1alpha1.InjectByName && container.Name == nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("name"), nil, "target name cannot be empty if targetPolicy=ByName"))
	}
	vmPatch := fldPath.Child("volumeMounts")
	for i, vm := range container.VolumeMounts {
		idxPath := vmPatch.Index(i)
		if len(vm.Name) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("name"), ""))
		}
		if len(vm.MountPath) == 0 {
			allErrs = append(allErrs, field.Required(idxPath.Child("mountPath"), ""))
		}
		if len(vm.SubPath) > 0 {
			allErrs = append(allErrs, validateLocalDescendingPath(vm.SubPath, idxPath.Child("subPath"))...)
		}
		if len(vm.SubPathExpr) > 0 {
			if len(vm.SubPath) > 0 {
				allErrs = append(allErrs, field.Invalid(idxPath.Child("subPathExpr"), vm.SubPathExpr, "subPathExpr and subPath are mutually exclusive"))
			}
			allErrs = append(allErrs, validateLocalDescendingPath(vm.SubPathExpr, idxPath.Child("subPathExpr"))...)
		}
	}
	//type []"k8s.io/api/core/v1".EnvVar) as the type []"k8s.io/kubernetes/pkg/apis/core
	if len(container.Env) > 0 {
		var coreEnvs []core.EnvVar
		for i, env := range container.Env {
			coreEnv := &core.EnvVar{}
			if err := k8scorev1.Convert_v1_EnvVar_To_core_EnvVar(env.DeepCopy(), coreEnv, nil); err != nil {
				allErrs = append(allErrs, field.InternalError(fldPath.Child("env").Index(i), err))
			}
			coreEnvs = append(coreEnvs, *coreEnv)
		}
		allErrs = append(allErrs,
			corevalidation.ValidateEnv(coreEnvs, fldPath.Child("env"), defaultValidationOptions)...)
	}
	return
}

// This validate will make sure targetPath:
// 1. is not abs path
// 2. does not have any element which is ".."
func validateLocalDescendingPath(targetPath string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if path.IsAbs(targetPath) {
		allErrs = append(allErrs, field.Invalid(fldPath, targetPath, "must be a relative path"))
	}

	allErrs = append(allErrs, validatePathNoBacksteps(targetPath, fldPath)...)

	return allErrs
}

// validatePathNoBacksteps makes sure the targetPath does not have any `..` path elements when split
//
// This assumes the OS of the apiserver and the nodes are the same. The same check should be done
// on the node to ensure there are no backsteps.
func validatePathNoBacksteps(targetPath string, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	parts := strings.Split(filepath.ToSlash(targetPath), "/")
	for _, item := range parts {
		if item == ".." {
			allErrs = append(allErrs, field.Invalid(fldPath, targetPath, "must not contain '..'"))
			break // even for `../../..`, one error is sufficient to make the point
		}
	}
	return allErrs
}
