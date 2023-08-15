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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/ruleset/register"
	"kusionstack.io/kafed/pkg/controllers/ruleset/utils"
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

func (c *checker) GetState(client client.Client, item client.Object) (CheckState, error) {

	result := CheckState{
		Passed: true,
	}
	stage := c.policy.Stage(item)
	if stage == "" {
		result.Info = "no stage found"
		return result, nil
	}

	rulesetNames := utils.GetRuleSets(item)
	fmt.Printf("%d\n", len(rulesetNames))
	// TODO: list ruleset, check match ruleset
	// TODO: add ruleset anno by webhook

	for _, name := range rulesetNames {
		rs := &appsv1alpha1.RuleSet{}
		if err := client.Get(context.TODO(), types.NamespacedName{Namespace: item.GetNamespace(), Name: name}, rs); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return result, err
		}
		findStatus := false
		for i, detail := range rs.Status.Details {
			if detail.Name != item.GetName() {
				continue
			}
			if detail.Stage != stage {
				break
			}
			findStatus = true
			if !detail.Passed {
				result.Passed = false
				result.Info += CollectInfo(name, rs.Status.Details[i])
			}
			result.States = append(result.States, State{
				RuleSetName: name,
				Detail:      *rs.Status.Details[i],
			})
		}
		if !findStatus {
			result.Passed = false
			result.Info = fmt.Sprintf("waiting for ruleset %s processing. ", rs.Name)
			return result, nil
		}
	}
	return result, nil
}

type CheckState struct {
	States []State
	Passed bool
	Info   string
}

func (cs *CheckState) InStage(stage string) bool {
	for _, state := range cs.States {
		if state.Detail.Stage != stage {
			return false
		}
	}
	return true
}

type State struct {
	RuleSetName string
	Detail      appsv1alpha1.Detail
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
