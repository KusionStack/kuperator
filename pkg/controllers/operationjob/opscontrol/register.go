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

package opscontrol

import "kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"

// registry of operationjob.spec.action handler
var actionRegistry map[string]ActionHandler

// registry of operationjob.spec.action podOpsLifecycle
var lifecycleAdapterRegistry map[string]podopslifecycle.LifecycleAdapter

func RegisterAction(action string, handler ActionHandler, lifecycleAdapter podopslifecycle.LifecycleAdapter) {
	if actionRegistry == nil {
		actionRegistry = make(map[string]ActionHandler)
	}
	actionRegistry[action] = handler

	if lifecycleAdapterRegistry == nil {
		lifecycleAdapterRegistry = make(map[string]podopslifecycle.LifecycleAdapter)
	}
	lifecycleAdapterRegistry[action] = lifecycleAdapter
}

func GetActionResources(action string) (ActionHandler, podopslifecycle.LifecycleAdapter) {
	var handler ActionHandler
	var lifecycleAdapter podopslifecycle.LifecycleAdapter
	if _, exist := actionRegistry[action]; exist {
		handler = actionRegistry[action]
	}
	if _, exist := lifecycleAdapterRegistry[action]; exist {
		lifecycleAdapter = lifecycleAdapterRegistry[action]
	}
	return handler, lifecycleAdapter
}
