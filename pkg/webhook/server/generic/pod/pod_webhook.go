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
	v1 "k8s.io/api/core/v1"

	"kusionstack.io/kafed/pkg/webhook/server/generic"
	"kusionstack.io/kafed/pkg/webhook/server/generic/pod/opslifecycle"
)

type NeedOpsLifecycle func(oldPod, newPod *v1.Pod) bool

type PodWebhook struct {
	mutatingHandler   *MutatingHandler
	validatingHandler *ValidatingHandler
}

func NewPodWebhook(needLifecycle NeedOpsLifecycle, readyToUpgrade opslifecycle.ReadyToUpgrade) *PodWebhook {
	return &PodWebhook{
		mutatingHandler:   NewMutatingHandler(needLifecycle, readyToUpgrade),
		validatingHandler: NewValidatingHandler(needLifecycle),
	}
}

func (h *PodWebhook) RegisterToDispatcher() {
	generic.MutatingTypeHandlerMap["Pod"] = h.mutatingHandler
	generic.ValidatingTypeHandlerMap["Pod"] = h.validatingHandler
}
