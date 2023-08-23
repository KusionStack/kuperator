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

package checker

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/ruleset/register"
)

type Check interface {
	// GetState get current check state
	GetState(client.Client, client.Object) (CheckState, error)
}

func NewCheck() Check {
	return &checker{policy: register.DefaultPolicy()}
}

type checker struct {
	policy register.Policy
}

// GetState get item current check state from all related ruleSets
func (c *checker) GetState(cl client.Client, item client.Object) (CheckState, error) {

	result := CheckState{}
	ruleSetList := &appsv1alpha1.RuleSetList{}
	if err := cl.List(context.TODO(), ruleSetList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(appsv1alpha1.FieldIndexRuleSet, item.GetName())}); err != nil {
		return result, err
	}
	for i := range ruleSetList.Items {
		rs := &ruleSetList.Items[i]
		findStatus := false
		for i, detail := range rs.Status.Details {
			if detail.Name != item.GetName() {
				continue
			}
			findStatus = true
			if !detail.Passed {
				result.Message += CollectInfo(rs.Name, rs.Status.Details[i])
			}
			result.States = append(result.States, State{
				RuleSetName: rs.Name,
				Detail:      rs.Status.Details[i],
			})
		}
		if !findStatus {
			result.States = append(result.States, State{
				RuleSetName: rs.Name,
				Detail: &appsv1alpha1.Detail{
					Passed: false,
				},
			})
			result.Message += fmt.Sprintf("[waiting for ruleset %s processing. ]", rs.Name)
			return result, nil
		}
	}
	return result, nil
}

type CheckState struct {
	States  []State
	Message string
}

func (cs *CheckState) InStage(stage string) bool {
	if len(cs.States) == 0 {
		return false
	}
	for _, state := range cs.States {
		if state.Detail.Stage != stage {
			return false
		}
	}
	return len(cs.States) > 0
}

func (cs *CheckState) InStageAndPassed(stage string) bool {
	for _, state := range cs.States {
		if state.Detail.Stage != stage || !state.Detail.Passed {
			return false
		}
	}
	return len(cs.States) > 0
}

type State struct {
	RuleSetName string
	Message     string
	Detail      *appsv1alpha1.Detail
}

func CollectInfo(ruleset string, detail *appsv1alpha1.Detail) string {
	res := ""
	for _, rej := range detail.RejectInfo {
		if res != "" {
			res = res + fmt.Sprintf(", %s:%s", rej.RuleName, rej.Reason)
		} else {
			res = fmt.Sprintf("%s:%s", rej.RuleName, rej.Reason)
		}
	}
	return fmt.Sprintf("[RuleSet: %s, RejectInfo: %s] ", ruleset, res)
}
