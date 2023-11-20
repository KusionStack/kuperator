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

package expectations

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestExpectation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	key := "test/foo"
	exp := NewControllerExpectations("test")
	ex, exist, err := exp.GetExpectations(key)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(exist).Should(gomega.BeFalse())
	g.Expect(ex).Should(gomega.BeNil())

	g.Expect(exp.InitExpectations(key)).Should(gomega.BeNil())
	ex, exist, err = exp.GetExpectations(key)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(exist).Should(gomega.BeTrue())
	g.Expect(ex).ShouldNot(gomega.BeNil())
	add, del := ex.GetExpectations()
	g.Expect(add).Should(gomega.BeEquivalentTo(0))
	g.Expect(del).Should(gomega.BeEquivalentTo(0))

	testcases := []struct {
		add         int
		del         int
		expectedAdd int
		expectedDel int
	}{
		{
			add:         1,
			del:         1,
			expectedAdd: 1,
			expectedDel: 1,
		},
		{
			add:         -1,
			del:         -1,
			expectedAdd: 0,
			expectedDel: 0,
		},
		{
			add:         -1,
			del:         -1,
			expectedAdd: -1,
			expectedDel: -1,
		},
	}

	for _, testcase := range testcases {
		exp.RaiseExpectations(key, testcase.add, testcase.del)
		ex, exist, err = exp.GetExpectations(key)
		g.Expect(err).Should(gomega.BeNil())
		g.Expect(exist).Should(gomega.BeTrue())
		g.Expect(ex).ShouldNot(gomega.BeNil())
		add, del = ex.GetExpectations()
		g.Expect(add).Should(gomega.BeEquivalentTo(testcase.expectedAdd))
		g.Expect(del).Should(gomega.BeEquivalentTo(testcase.expectedDel))
	}

	exp.DeleteExpectations(key)
	ex, exist, err = exp.GetExpectations(key)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(exist).Should(gomega.BeFalse())
	g.Expect(ex).Should(gomega.BeNil())
	g.Expect(exp.SatisfiedExpectations(key)).Should(gomega.BeTrue())
}

func TestExpectationSatisfied(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	key := "test/foo"
	exp := NewControllerExpectations("test")
	g.Expect(exp.InitExpectations(key)).Should(gomega.BeNil())

	g.Expect(exp.ExpectCreations(key, 2)).Should(gomega.BeNil())
	exp.CreationObserved(key)
	g.Expect(exp.SatisfiedExpectations(key)).Should(gomega.BeFalse())
	exp.CreationObserved(key)
	g.Expect(exp.SatisfiedExpectations(key)).Should(gomega.BeTrue())

	g.Expect(exp.ExpectDeletions(key, 2)).Should(gomega.BeNil())
	exp.CreationObserved(key)
	g.Expect(exp.SatisfiedExpectations(key)).Should(gomega.BeFalse())
	exp.DeletionObserved(key)
	g.Expect(exp.SatisfiedExpectations(key)).Should(gomega.BeFalse())
	exp.DeletionObserved(key)
	g.Expect(exp.SatisfiedExpectations(key)).Should(gomega.BeTrue())
}
