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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCache(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	ca := newCache()
	ca.RegisterStage("stage-a", func(obj client.Object) bool {
		return true
	})
	ca.RegisterStage("stage-b", func(obj client.Object) bool {
		return false
	})
	g.Expect(ca.InStage(nil, "stage-a")).Should(gomega.BeTrue())
	g.Expect(ca.InStage(nil, "stage-b")).Should(gomega.BeFalse())
	g.Expect(len(ca.GetStages())).Should(gomega.Equal(2))
	ca.RegisterCondition("condition-a", func(obj client.Object) bool {
		return true
	})
	ca.RegisterCondition("condition-b", func(obj client.Object) bool {
		return false
	})
	g.Expect(len(ca.Conditions(nil))).Should(gomega.Equal(1))
	g.Expect(ca.Conditions(nil)[0]).Should(gomega.Equal("condition-a"))
	g.Expect(len(ca.MatchConditions(nil, "xxx", "condition-a", "condition-b"))).Should(gomega.Equal(1))
}
