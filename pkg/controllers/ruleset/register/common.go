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

package register

import (
	corev1 "k8s.io/api/core/v1"

	"kusionstack.io/kafed/pkg/controllers/utils"
)

var (
	UnAvailableFuncList []UnAvailableFunc
)

func AddUnAvailableFunc(f func(pod *corev1.Pod) (bool, *int32)) {
	UnAvailableFuncList = append(UnAvailableFuncList, f)
}

type UnAvailableFunc func(pod *corev1.Pod) (bool, *int32)

func DefaultInit() {
	UnAvailableFuncList = append(UnAvailableFuncList, func(pod *corev1.Pod) (bool, *int32) {
		return !utils.IsPodReady(pod) || utils.IsPodTerminal(pod), nil
	})
}
