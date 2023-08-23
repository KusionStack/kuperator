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
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	return MoveRulesetAnno(po, rulesetName) || MoveDetailAnno(po, rulesetName)
}

// MoveRulesetAnno move RuleSet name in annotation ruleset.kusionstack.io/rulesets
func MoveRulesetAnno(po *corev1.Pod, rulesetName string) bool {

	if po.Annotations == nil {
		return false
	}
	val, ok := po.Annotations[appsv1alpha1.AnnotationRuleSets]
	if !ok {
		return false
	}
	state := RuleSets{}
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		klog.Errorf("fail to unmarshal anno %s=%s, %v", appsv1alpha1.AnnotationRuleSets, val, err)
		return false
	}
	for i, name := range state {
		if name == rulesetName {
			state = append(state[:i], state[i+1:]...)
			setRulesetAnno(po, state)
			return true
		}
	}
	return false
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

type RuleSets []string

func AddRuleSetAnno(po *corev1.Pod, rulesetName string) bool {
	if po.Annotations == nil {
		po.Annotations = map[string]string{}
	}
	val, ok := po.Annotations[appsv1alpha1.AnnotationRuleSets]
	if !ok {
		po.Annotations[appsv1alpha1.AnnotationRuleSets] = annoVal(rulesetName)
		return true
	}
	rs := &RuleSets{}
	if err := json.Unmarshal([]byte(val), rs); err != nil {
		klog.Errorf("fail to unmarshal anno %s=%s, %v", appsv1alpha1.AnnotationRuleSets, val, err)
		po.Annotations[appsv1alpha1.AnnotationRuleSets] = annoVal(rulesetName)
		return true
	}
	for _, name := range *rs {
		if name == rulesetName {
			return false
		}
	}
	*rs = append(*rs, rulesetName)
	bt, _ := json.Marshal(rs)
	po.Annotations[appsv1alpha1.AnnotationRuleSets] = string(bt)
	return true
}

func GetRuleSets(item client.Object) (result []string) {
	item.GetAnnotations()
	if item.GetAnnotations() == nil {
		return result
	}
	val, ok := item.GetAnnotations()[appsv1alpha1.AnnotationRuleSets]
	if !ok || len(val) == 0 {
		return result
	}
	rs := &RuleSets{}
	if err := json.Unmarshal([]byte(val), rs); err != nil {
		klog.Errorf("fail to get RuleSets from item %s/%s anno %s, %v", item.GetNamespace(), item.GetName(), val, err)
		return result
	}
	return *rs
}

func annoVal(names ...string) string {
	var rs RuleSets
	rs = append(rs, names...)
	val, _ := json.Marshal(rs)
	return string(val)
}

func setRulesetAnno(po *corev1.Pod, rs RuleSets) {
	val, _ := json.Marshal(rs)
	po.Annotations[appsv1alpha1.AnnotationRuleSets] = string(val)
}
