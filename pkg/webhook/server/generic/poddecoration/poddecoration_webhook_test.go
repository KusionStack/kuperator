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

package poddecoration

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

var _ = Describe("PodDecoration webhook", func() {
	It("test validating", func() {
		pd := &appsv1alpha1.PodDecoration{
			Spec: appsv1alpha1.PodDecorationSpec{
				Template: appsv1alpha1.PodDecorationPodTemplate{
					PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
						{
							TargetPolicy:                  appsv1alpha1.InjectByName,
							PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{},
						},
					},
				},
			},
		}
		Expect(ValidatePodDecoration(pd)).Should(HaveOccurred())
	})
	It("test mutating", func() {
		pd := &appsv1alpha1.PodDecoration{
			Spec: appsv1alpha1.PodDecorationSpec{
				Template: appsv1alpha1.PodDecorationPodTemplate{
					PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
						{
							PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
								Env: []corev1.EnvVar{
									{
										Name: "env",
									},
								},
							},
						},
					},
					Metadata: []*appsv1alpha1.PodDecorationPodTemplateMeta{
						{
							Labels: map[string]string{"a": "b"},
						},
					},
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							Container: corev1.Container{
								Name: "foo",
							},
						},
					},
				},
			},
		}
		SetDefaultPodDecoration(pd)
		Expect(pd.Spec.Template.PrimaryContainers[0].TargetPolicy).ShouldNot(BeEquivalentTo(""))
		Expect(pd.Spec.Template.Containers[0].InjectPolicy).ShouldNot(BeEquivalentTo(""))
		Expect(pd.Spec.Template.Metadata[0].PatchPolicy).ShouldNot(BeEquivalentTo(""))
	})
})

func TestPodDecorationWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "test PodDecoration webhook")
}
