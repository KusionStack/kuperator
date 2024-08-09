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
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func IsPodPassRule(podName string, podtransitionrule *appsv1alpha1.PodTransitionRule, rule string) bool {
	passedRules := GetPodPassedRules(podName, podtransitionrule)
	return passedRules.Has(rule)
}

func GetPodPassedRules(podName string, podtransitionrule *appsv1alpha1.PodTransitionRule) (rules sets.String) {
	rules = sets.NewString()
	if podtransitionrule.Status.Details == nil {
		return rules
	}
	for _, detail := range podtransitionrule.Status.Details {
		if detail.Name != podName {
			continue
		}
		rules.Insert(detail.PassedRules...)
	}
	return rules
}
