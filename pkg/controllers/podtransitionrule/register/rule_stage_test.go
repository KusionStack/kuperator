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
	"testing"

	"github.com/onsi/gomega"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func TestStage(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	def := appsv1alpha1.TransitionRuleDefinition{
		AvailablePolicy: &appsv1alpha1.AvailableRule{},
	}
	InitDefaultRuleStage(&def, "test-stage")
	g.Expect(GetRuleStage(&def)).Should(gomega.BeEquivalentTo("test-stage"))
	g.Expect(GetRuleType(&def)).Should(gomega.BeEquivalentTo("AvailablePolicy"))
}
