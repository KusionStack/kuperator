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

// ActionRegistry is a map of operationjob.spec.action to actionHandler
var ActionRegistry map[string]ActionHandler

// EnablePodOpsLifecycleRegistry is a map of operationjob.spec.action to enablePodOpsLifecycle
var EnablePodOpsLifecycleRegistry map[string]bool

// RegisterAction will register an operationJob action with handler and lifecycleAdapter
// Note: if enablePodOpsLifecycle=false, this operation will be done directly, ignoring podOpsLifecycle
func RegisterAction(action string, handler ActionHandler, enablePodOpsLifecycle bool) {
	if ActionRegistry == nil {
		ActionRegistry = make(map[string]ActionHandler)
	}
	ActionRegistry[action] = handler

	if EnablePodOpsLifecycleRegistry == nil {
		EnablePodOpsLifecycleRegistry = make(map[string]bool)
	}
	EnablePodOpsLifecycleRegistry[action] = enablePodOpsLifecycle
}

func GetActionResources(action string) (ActionHandler, bool) {
	var handler ActionHandler
	var enablePodOpsLifecycle bool
	if _, exist := ActionRegistry[action]; exist {
		handler = ActionRegistry[action]
	}
	if _, exist := EnablePodOpsLifecycleRegistry[action]; exist {
		enablePodOpsLifecycle = EnablePodOpsLifecycleRegistry[action]
	}
	return handler, enablePodOpsLifecycle
}
