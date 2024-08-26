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

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/register"
	"kusionstack.io/kuperator/pkg/utils/inject"
)

type Checker interface {
	// GetState collects current states from all associated PodTransitionRules, and every state stage is not guaranteed to be consistent
	GetState(context.Context, client.Client, client.Object) (CheckState, error)
}

func NewCheck() Checker {
	return &checker{policy: register.DefaultPolicy()}
}

type checker struct {
	policy register.Policy
}

// GetState get item current check state from all related podTransitionRules
func (c *checker) GetState(ctx context.Context, cl client.Client, item client.Object) (CheckState, error) {

	result := CheckState{
		Stage: c.policy.Stage(item),
	}
	podTransitionRuleList := &appsv1alpha1.PodTransitionRuleList{}
	if err := cl.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, item.GetName())}); err != nil {
		return result, err
	}
	for i := range podTransitionRuleList.Items {
		rs := &podTransitionRuleList.Items[i]
		findStatus := false
		for j, detail := range rs.Status.Details {
			if detail.Name != item.GetName() {
				continue
			}
			findStatus = true
			if !detail.Passed {
				result.Message += CollectInfo(rs.Name, rs.Status.Details[j])
			}
			result.States = append(result.States, State{
				PodTransitionRuleName: rs.Name,
				Detail:                rs.Status.Details[j],
			})
			break
		}
		if !findStatus {
			result.States = append(result.States, State{
				PodTransitionRuleName: rs.Name,
				Detail: &appsv1alpha1.PodTransitionDetail{
					Passed: true,
				},
			})
			result.Message += fmt.Sprintf("[waiting for podtransitionrule %s processing. ]", rs.Name)
		}
	}
	if len(podTransitionRuleList.Items) == 0 {
		result.Message = "No podTransitionRules found"
	}
	return result, nil
}

type CheckState struct {
	Stage   string
	States  []State
	Message string
}

func (cs *CheckState) InStage() bool {
	for _, state := range cs.States {
		if state.Detail.Stage != cs.Stage {
			return false
		}
	}
	return true
}

func (cs *CheckState) InStageAndPassed() bool {
	for _, state := range cs.States {
		if state.Detail.Stage != cs.Stage || !state.Detail.Passed {
			return false
		}
	}
	return true
}

type State struct {
	PodTransitionRuleName string
	Message               string
	Detail                *appsv1alpha1.PodTransitionDetail
}

func CollectInfo(podtransitionrule string, detail *appsv1alpha1.PodTransitionDetail) string {
	res := ""
	for _, rej := range detail.RejectInfo {
		if res != "" {
			res = res + fmt.Sprintf(", %s:%s", rej.RuleName, rej.Reason)
		} else {
			res = fmt.Sprintf("%s:%s", rej.RuleName, rej.Reason)
		}
	}
	return fmt.Sprintf("[PodTransitionRule: %s, RejectInfo: %s] ", podtransitionrule, res)
}
