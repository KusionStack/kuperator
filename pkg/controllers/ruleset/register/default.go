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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func DefaultPolicy() Policy {
	return defaultCache
}

func DefaultRegister() Register {
	return defaultCache
}

type Policy interface {
	Stage(obj client.Object) string
	InStage(obj client.Object, key string) bool
	GetStages() []string
	Conditions(obj client.Object) []string
	MatchConditions(obj client.Object, conditions ...string) []string
}

type Register interface {
	RegisterStage(key string, inStage func(obj client.Object) bool) error
	RegisterCondition(opsCondition string, inCondition func(obj client.Object) bool) error
}
