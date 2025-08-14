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
	"reflect"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

var ruleStage = map[string]string{}

func InitDefaultRuleStage(ruleDef *appsv1alpha1.TransitionRuleDefinition, stage string) {
	ruleStage[GetRuleType(ruleDef)] = stage
}

func GetRuleStage(ruleDef *appsv1alpha1.TransitionRuleDefinition) string {
	stage := ruleStage[GetRuleType(ruleDef)]
	if stage == "" && len(defaultCache.GetStages()) > 0 {
		stage = defaultCache.GetStages()[0]
	}
	return stage
}

func GetRuleType(ruleDef *appsv1alpha1.TransitionRuleDefinition) string {
	typRule := reflect.TypeOf(*ruleDef)
	valRule := reflect.ValueOf(*ruleDef)
	fCount := valRule.NumField()
	for i := 0; i < fCount; i++ {
		if valRule.Field(i).IsNil() {
			continue
		}
		return typRule.Field(i).Name
	}
	return ""
}
