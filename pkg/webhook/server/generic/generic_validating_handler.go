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

var _ inject.Injector = &ValidatingHandler{}
var _ inject.Client = &ValidatingHandler{}
var _ inject.Logger = &ValidatingHandler{}
var _ admission.DecoderInjector = &ValidatingHandler{}

// ValidatingHandler validates all resources requests
type ValidatingHandler struct {
	*mixin.WebhookHandlerMixin

	// setFields allows injecting dependencies from an external source
	setFields inject.Func
}

func NewGenericValidatingHandler() *ValidatingHandler {
	return &ValidatingHandler{
		WebhookHandlerMixin: mixin.NewWebhookHandlerMixin(),
	}
}

// Handle handles admission requests.
func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	key := req.Kind.Kind
	if req.SubResource != "" {
		key = fmt.Sprintf("%s/%s", req.Kind.Kind, req.SubResource)
	}

	h.Logger.V(5).Info("validating handler",
		"kind", req.Kind.Kind,
		"key", commonutils.AdmissionRequestObjectKeyString(req),
		"op", req.Operation,
	)
	h.Logger.Info("validating", key)

	if handler, exist := ValidatingTypeHandlerMap[key]; exist {
		return handler.Handle(ctx, req)
	}

	return admission.ValidationResponse(true, "")
}

func (h *ValidatingHandler) InjectFunc(f inject.Func) error {
	h.setFields = f

	for _, handler := range ValidatingTypeHandlerMap {
		if err := h.setFields(handler); err != nil {
			return err
		}
	}
	return nil
}

func (h *ValidatingHandler) InjectLogger(l logr.Logger) error {
	_ = h.WebhookHandlerMixin.InjectLogger(l)

	// inject logger into subHandlers
	for kind, handler := range ValidatingTypeHandlerMap {
		if _, err := inject.LoggerInto(l.WithValues("kind", kind), handler); err != nil {
			return err
		}
	}
	return nil
}
