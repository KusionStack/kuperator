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

func TestResourceVersionExpectation(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	key := "test"

	exp := NewResourceVersionExpectation()
	exp.SetExpectations(key, "1")
	item, ok, err := exp.GetExpectations(key)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(ok).Should(gomega.BeTrue())
	g.Expect(item.resourceVersion).Should(gomega.Equal(int64(1)))

	exp.ExpectUpdate(key, "2")
	item, ok, err = exp.GetExpectations(key)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(ok).Should(gomega.BeTrue())
	g.Expect(item.resourceVersion).Should(gomega.Equal(int64(2)))

	ok = exp.SatisfiedExpectations(key, "1")
	g.Expect(ok).Should(gomega.BeFalse())
	ok = exp.SatisfiedExpectations(key, "2")
	g.Expect(ok).Should(gomega.BeFalse())
	ok = exp.SatisfiedExpectations(key, "3")
	g.Expect(ok).Should(gomega.BeTrue())

	exp.DeleteExpectations(key)
	item, ok, err = exp.GetExpectations(key)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(ok).Should(gomega.BeFalse())
	g.Expect(item).Should(gomega.BeNil())
	g.Expect(exp.SatisfiedExpectations(key, "1")).Should(gomega.BeTrue())
}
