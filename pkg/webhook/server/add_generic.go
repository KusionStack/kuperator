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

package server

import (
	"fmt"

	"k8s.io/klog/v2"

	"kusionstack.io/kafed/pkg/webhook/server/generic"
	_ "kusionstack.io/kafed/pkg/webhook/server/generic/pod"
)

func init() {
	for k, v := range generic.HandlerMap {
		_, found := HandlerMap[k]
		if found {
			klog.V(1).Info(fmt.Sprintf(
				"conflicting webhook builder names in handler map: %v", k))
		}
		HandlerMap[k] = v
	}
}
