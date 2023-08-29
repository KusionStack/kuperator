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
	"kusionstack.io/kafed/pkg/webhook/server/generic/collaset"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	webhookdmission "kusionstack.io/kafed/pkg/webhook/admission"
	"kusionstack.io/kafed/pkg/webhook/server/generic/pod"
	"kusionstack.io/kafed/pkg/webhook/server/generic/ruleset"
)

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"mutating-generic":   &MutatingHandler{},
		"validating-generic": &ValidatingHandler{},
	}
)

var MutatingTypeHandlerMap = map[string]webhookdmission.DispatchHandler{}
var ValidatingTypeHandlerMap = map[string]webhookdmission.DispatchHandler{}

func init() {
	podMutatingHandler := pod.NewMutatingHandler()
	MutatingTypeHandlerMap["Pod"] = podMutatingHandler
	MutatingTypeHandlerMap["Pod/status"] = podMutatingHandler
	ValidatingTypeHandlerMap["Pod"] = pod.NewValidatingHandler()

	MutatingTypeHandlerMap["RuleSet"] = ruleset.NewMutatingHandler()
	ValidatingTypeHandlerMap["RuleSet"] = ruleset.NewValidatingHandler()

	MutatingTypeHandlerMap["CollaSet"] = collaset.NewMutatingHandler()
	ValidatingTypeHandlerMap["CollaSet"] = collaset.NewValidatingHandler()
}
