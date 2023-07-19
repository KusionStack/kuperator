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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ValidatingHandler validates all resources requests
type ValidatingHandler struct {
	Client client.Client
	// Decoder decodes objects
	Decoder *admission.Decoder
}

// Handle handles admission requests.
func (h *ValidatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	key := req.Kind.Kind
	if req.SubResource != "" {
		key = fmt.Sprintf("%s/%s", req.Kind.Kind, req.SubResource)
	}
	if handler, exist := ValidatingTypeHandlerMap[key]; exist {
		return handler.Handle(ctx, req, h.Client, h.Decoder)
	}

	return admission.ValidationResponse(true, "")
}

var _ inject.Client = &ValidatingHandler{}

// InjectClient injects the client into the ValidatingHandler
func (h *ValidatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

var _ admission.DecoderInjector = &ValidatingHandler{}

// InjectDecoder injects the decoder into the ValidatingHandler
func (h *ValidatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
