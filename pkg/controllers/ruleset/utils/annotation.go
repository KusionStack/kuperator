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
	"encoding/json"

	corev1 "k8s.io/api/core/v1"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

func HasSkipRule(po *corev1.Pod, ruleName string) (bool, error) {
	if po.Annotations == nil || len(po.Annotations[appsv1alpha1.AnnotationPodSkipRuleConditions]) == 0 {
		return false, nil
	}
	podSkipRuleConditions := &PodSkipRuleConditions{}
	if err := json.Unmarshal([]byte(po.Annotations[appsv1alpha1.AnnotationPodSkipRuleConditions]), podSkipRuleConditions); err != nil {
		return false, err
	}
	if podSkipRuleConditions.SkipRules == nil {
		return false, nil
	}
	for _, skipRuleName := range podSkipRuleConditions.SkipRules {
		if skipRuleName == ruleName {
			return true, nil
		}
	}
	return false, nil
}

type PodSkipRuleConditions struct {
	SkipRules []string `json:"skipRules,omitempty"`
}

func MoveAllRuleSetInfo(po *corev1.Pod, rulesetName string) bool {
	return MoveDetailAnno(po, rulesetName)
}

// MoveDetailAnno move RuleSet detail annotation ruleset.kusionstack.io/detail-${ruleSetName}
func MoveDetailAnno(po *corev1.Pod, rulesetName string) bool {
	if po.Annotations == nil {
		return false
	}
	_, ok := po.Annotations[appsv1alpha1.AnnotationRuleSetDetailPrefix+rulesetName]
	if ok {
		delete(po.Annotations, appsv1alpha1.AnnotationRuleSetDetailPrefix+rulesetName)
	}
	return ok
}
