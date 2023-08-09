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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"kusionstack.io/kafed/pkg/log"
	"kusionstack.io/kafed/pkg/webhook/server/generic/pod/opslifecycle"
)

const (
	mutatingName = "pod-mutating-webhook"
)

var (
	logger = log.New(logf.Log.WithName(mutatingName).WithValues("kind", "pod-mutating-webhook"))
)

type MutatingHandler struct {
	needLifecycle NeedOpsLifecycle
	opsLifecycle  *opslifecycle.OpsLifecycle
}

func NewMutatingHandler(needLifecycle NeedOpsLifecycle, readyToUpgrade opslifecycle.ReadyToUpgrade) *MutatingHandler {
	return &MutatingHandler{
		needLifecycle: needLifecycle,
		opsLifecycle:  opslifecycle.New(readyToUpgrade),
	}
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request, c client.Client, decoder *admission.Decoder) (resp admission.Response) {
	if req.Kind.Kind != "Pod" {
		return admission.Patched("Invalid kind")
	}

	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return admission.Patched("Not Create or Update, but " + string(req.Operation))
	}

	pod := &corev1.Pod{}
	err := decoder.Decode(req, pod)
	if err != nil {
		logger.Errorf(fmt.Sprintf("decode request failed, error: %s", err))
		return admission.Errored(http.StatusBadRequest, err)
	}

	if req.Operation == admissionv1.Create {
		if !h.needLifecycle(nil, pod) {
			return admission.Patched("Not need opslifecycle mutating")
		}
	}

	if req.Operation == admissionv1.Update {
		old := &corev1.Pod{}
		if err := decoder.DecodeRaw(req.OldObject, old); err != nil {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("fail to unmarshal old object: %s", err))
		}

		if !h.needLifecycle(old, pod) {
			return admission.Patched("Not need opslifecycle mutating")
		}

		if err := h.opsLifecycle.Mutating(ctx, old, pod, c, logger); err != nil {
			logger.Errorf("fail to start over new opslifecycle for pod %s/%s: %s", pod.Namespace, pod.Name, err)
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	marshalled, err := json.Marshal(pod)
	if err != nil {
		logger.Errorf(fmt.Sprintf("marshal Pod failed, error: %s", err))
		return admission.Errored(http.StatusBadRequest, err)
	}

	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}
