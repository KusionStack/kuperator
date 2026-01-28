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

package apps

import (
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"kusionstack.io/kuperator/test/e2e/framework"
)

// SIGDescribe annotates the test with the SIG label.
func SIGDescribe(text string, body func()) bool {
	return ginkgo.Describe("[apps] "+text, body)
}

func afterEach(tester *framework.CollaSetTester, ns string) {
	gomega.Expect(tester.DeleteAllCollaSet(ns)).NotTo(gomega.HaveOccurred())
	gomega.Eventually(func() bool {
		clsList, err := tester.GetAllCollaSet(ns)
		if err != nil {
			return false
		}
		return len(clsList.Items) == 0
	}, 30*time.Second, 3*time.Second).Should(gomega.Equal(true))
}
