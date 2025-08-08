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

package generic

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	commonutils "kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

var (
	_ inject.Injector           = &MutatingHandler{}
	_ inject.Client             = &MutatingHandler{}
	_ inject.Logger             = &MutatingHandler{}
	_ admission.DecoderInjector = &MutatingHandler{}
)

// MutatingHandler handles all resources mutating operation
type MutatingHandler struct {
	*mixin.WebhookHandlerMixin

	// setFields allows injecting dependencies from an external source
	setFields inject.Func
}

func NewGenericMutatingHandler() *MutatingHandler {
	return &MutatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.DryRun != nil && *req.DryRun {
		return admission.Allowed("dry run")
	}

	key := req.Kind.Kind
	if req.SubResource != "" {
		key = fmt.Sprintf("%s/%s", req.Kind.Kind, req.SubResource)
	}

	h.Logger.V(5).Info("mutating handler",
		"kind", req.Kind.Kind,
		"key", commonutils.AdmissionRequestObjectKeyString(req),
		"op", req.Operation,
	)

	if handler, exist := MutatingTypeHandlerMap[key]; exist {
		return handler.Handle(ctx, req)
	}

	// do nothing
	return admission.Patched("NoMutating")
}

func (h *MutatingHandler) InjectFunc(f inject.Func) error {
	h.setFields = f

	for i := range MutatingTypeHandlerMap {
		if err := h.setFields(MutatingTypeHandlerMap[i]); err != nil {
			return err
		}
	}
	return nil
}

func (h *MutatingHandler) InjectLogger(l logr.Logger) error {
	_ = h.WebhookHandlerMixin.InjectLogger(l)

	// inject logger into subHandlers
	for kind, handler := range MutatingTypeHandlerMap {
		if _, err := inject.LoggerInto(l.WithValues("kind", kind), handler); err != nil {
			return err
		}
	}
	return nil
}
