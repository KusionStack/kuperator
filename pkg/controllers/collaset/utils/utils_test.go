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
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CollaSets utils tests")
}

var _ = Describe("Condition tests", func() {
	It("test AddOrUpdateCondition", func() {
		status := &appsv1alpha1.CollaSetStatus{
			Conditions: []appsv1alpha1.CollaSetCondition{
				{
					Type:   appsv1alpha1.CollaSetScale,
					Status: corev1.ConditionTrue,
				},
			},
		}
		AddOrUpdateCondition(status, appsv1alpha1.CollaSetScale, fmt.Errorf("test err"), "", "")
		AddOrUpdateCondition(status, appsv1alpha1.CollaSetUpdate, nil, "", "")
		Expect(len(status.Conditions)).Should(Equal(2))
	})
})
