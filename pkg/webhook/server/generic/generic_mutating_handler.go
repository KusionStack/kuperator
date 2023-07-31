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

// MutatingHandler handles all resources mutating operation
type MutatingHandler struct {
	Client client.Client
	// Decoder decodes objects
	Decoder *admission.Decoder
}

func (h *MutatingHandler) Handle(ctx context.Context, req admission.Request) (resp admission.Response) {
	if req.DryRun != nil && *req.DryRun {
		return admission.Allowed("dry run")
	}

	key := req.Kind.Kind
	if req.SubResource != "" {
		key = fmt.Sprintf("%s/%s", req.Kind.Kind, req.SubResource)
	}
	if handler, exist := MutatingTypeHandlerMap[key]; exist {
		return handler.Handle(ctx, req, h.Client, h.Decoder)
	}

	// do nothing
	return admission.Patched("NoMutating")
}

var _ inject.Client = &MutatingHandler{}

// InjectClient injects the client into the MutatingHandler
func (h *MutatingHandler) InjectClient(c client.Client) error {
	h.Client = c
	return nil
}

var _ admission.DecoderInjector = &MutatingHandler{}

// InjectDecoder injects the decoder into the MutatingHandler
func (h *MutatingHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}
