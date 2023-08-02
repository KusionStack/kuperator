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

package pod

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"kusionstack.io/kafed/pkg/webhook/server/generic/pod/opslifecycle"
)

type ValidatingHandler struct {
	needOpsLifecycle NeedOpsLifecycle
	opslifecycle     *opslifecycle.OpsLifecycle
}

func NewValidatingHandler(needOpsLifecycle NeedOpsLifecycle) *ValidatingHandler {
	return &ValidatingHandler{
		needOpsLifecycle: needOpsLifecycle,
	}
}

func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request, client client.Client, decoder *admission.Decoder) (resp admission.Response) {
	if req.Operation == admissionv1.Delete {
		return admission.Allowed("pod is allowed by opslifecycle")
	}

	obj := &corev1.Pod{}
	if err := decoder.Decode(req, obj); err != nil {
		s, _ := json.Marshal(req)
		return admission.Errored(http.StatusBadRequest, fmt.Errorf("fail to decode old object from request %s: %s", s, err))
	}
	if !h.needOpsLifecycle(nil, obj) {
		return admission.Allowed("pod is allowed by opslifecycle")
	}

	if err := h.opslifecycle.Validating(ctx, obj, logger); err != nil {
		return admission.Denied("pod is not allowed by opslifecycle")
	}
	return admission.Allowed("pod is allowed by opslifecycle")
}
