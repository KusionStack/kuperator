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

package podcontext

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func init() {
	corev1.AddToScheme(scheme)
	appsv1alpha1.AddToScheme(scheme)
}

var (
	scheme = runtime.NewScheme()
)

var _ = Describe("ResourceContext allocation", func() {

	It("allocate ID", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		namespace := "test"

		instance1 := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "foo1",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				ScaleStrategy: appsv1alpha1.ScaleStrategy{
					Context: "foo", // use the same Context
				},
			},
		}

		instance2 := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "foo2",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				ScaleStrategy: appsv1alpha1.ScaleStrategy{
					Context: "foo", // use the same Context
				},
			},
		}

		ownedIDs, err := AllocateID(c, instance1, "", 10)
		Expect(err).Should(BeNil())
		Expect(len(ownedIDs)).Should(BeEquivalentTo(10))
		for i := 0; i < 10; i++ {
			_, exist := ownedIDs[i]
			Expect(exist).Should(BeTrue())
		}

		ownedIDs, err = AllocateID(c, instance2, "", 4)
		Expect(err).Should(BeNil())
		Expect(len(ownedIDs)).Should(BeEquivalentTo(4))
		for i := 10; i < 14; i++ {
			_, exist := ownedIDs[i]
			Expect(exist).Should(BeTrue())
		}

		ownedIDs, err = AllocateID(c, instance1, "", 10)
		delete(ownedIDs, 4)
		delete(ownedIDs, 6)
		Expect(UpdateToPodContext(c, instance1, ownedIDs)).Should(BeNil())

		ownedIDs, err = AllocateID(c, instance2, "", 7)
		Expect(err).Should(BeNil())
		Expect(len(ownedIDs)).Should(BeEquivalentTo(7))
		for _, i := range []int{4, 6, 10, 11, 12, 13, 14} {
			_, exist := ownedIDs[i]
			Expect(exist).Should(BeTrue())
		}

		ownedIDs, err = AllocateID(c, instance1, "", 12)
		Expect(err).Should(BeNil())
		Expect(len(ownedIDs)).Should(BeEquivalentTo(12))
		for _, i := range []int{0, 1, 2, 3, 5, 7, 8, 9, 15, 16, 17, 18} {
			_, exist := ownedIDs[i]
			Expect(exist).Should(BeTrue())
		}
	})

})

func TestPodContext(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ResourceContext Test Suite")
}
