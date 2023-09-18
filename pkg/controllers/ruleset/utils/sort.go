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

package utils

import (
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type Rules []*appsv1alpha1.RuleSetRule

func (e Rules) Len() int      { return len(e) }
func (e Rules) Swap(i, j int) { e[i], e[j] = e[j], e[i] }
func (e Rules) Less(i, j int) bool {
	return weight(e[i]) < weight(e[j])
}

func weight(rule *appsv1alpha1.RuleSetRule) int {

	if rule.AvailablePolicy != nil {
		return 1
	}
	if rule.LabelCheck != nil {
		return 3
	}

	if rule.Webhook != nil {
		return 5
	}

	return 100
}
