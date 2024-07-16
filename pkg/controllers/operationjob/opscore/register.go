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

package opscore

// registry map of operationjob.spec.action to actionHandler
var actionRegistry map[string]ActionHandler

// registry map of operationjob.spec.action to enablePodOpsLifecycle
var enablePodOpsLifecycleRegistry map[string]bool

// RegisterAction will register an operationJob action with handler and lifecycleAdapter
// Note: if enablePodOpsLifecycle=false, this operation will be done directly, ignoring podOpsLifecycle
func RegisterAction(action string, handler ActionHandler, enablePodOpsLifecycle bool) {
	if actionRegistry == nil {
		actionRegistry = make(map[string]ActionHandler)
	}
	actionRegistry[action] = handler

	if enablePodOpsLifecycleRegistry == nil {
		enablePodOpsLifecycleRegistry = make(map[string]bool)
	}
	enablePodOpsLifecycleRegistry[action] = enablePodOpsLifecycle
}

func GetActionResources(action string) (ActionHandler, bool) {
	var handler ActionHandler
	var enablePodOpsLifecycle bool
	if _, exist := actionRegistry[action]; exist {
		handler = actionRegistry[action]
	}
	if _, exist := enablePodOpsLifecycleRegistry[action]; exist {
		enablePodOpsLifecycle = enablePodOpsLifecycleRegistry[action]
	}
	return handler, enablePodOpsLifecycle
}
