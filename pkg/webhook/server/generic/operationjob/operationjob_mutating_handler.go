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

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/operationjob/recreate"
	commonutils "kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
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
	if req.Operation != admissionv1.Update {
		return admission.Allowed("")
	}

	var instance, old appsv1alpha1.OperationJob

	logger := h.Logger.WithValues(
		"op", req.Operation,
		"operationjob", commonutils.AdmissionRequestObjectKeyString(req),
	)

	if err := h.Decoder.Decode(req, &instance); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if err := h.Decoder.DecodeRaw(req.OldObject, &old); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	currDetailMap := make(map[string][]string)
	for _, target := range instance.Spec.Targets {
		currDetailMap[target.PodName] = target.Containers
	}

	oldDetailMap := make(map[string][]string)
	for _, target := range old.Spec.Targets {
		oldDetailMap[target.PodName] = target.Containers
	}

	for podName, containers := range oldDetailMap {
		if _, exist := currDetailMap[podName]; !exist {
			return admission.Denied(fmt.Sprintf("not allowed to remove pod target %s", podName))
		}
		if instance.Spec.Action == appsv1alpha1.OpsActionRecreate {
			if !stringArrayEqual(currDetailMap[podName], containers) {
				return admission.Denied(fmt.Sprintf("containers list in target is immutable %v", containers))
			}
		}
	}

	if instance.Spec.Action != appsv1alpha1.OpsActionRecreate {
		return admission.Allowed("")
	}

	if instance.ObjectMeta.Annotations == nil {
		instance.ObjectMeta.Annotations = make(map[string]string)
	}
	if _, exist := instance.ObjectMeta.Annotations[appsv1alpha1.AnnotationOperationJobRecreateMethod]; !exist {
		instance.ObjectMeta.Annotations[appsv1alpha1.AnnotationOperationJobRecreateMethod] = recreate.KruiseCcontainerRecreateRequest
		marshalled, err := json.Marshal(instance)
		if err != nil {
			logger.Error(err, "failed to marshal collaset to json")
			return admission.Errored(http.StatusInternalServerError, err)
		}
		return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
	}

	return admission.Allowed("")
}

func stringArrayEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
