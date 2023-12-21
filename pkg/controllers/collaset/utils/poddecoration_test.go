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
	"context"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func TestPodDecorationUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CollaSetController Test Suite")
}

var _ = Describe("PodDecoration utils", func() {
	It("Test PodDecorationGetter", func() {
		getter := &podDecorationGetter{
			latestPodDecorationNames: sets.NewString("foo-1", "foo-2"),
			revisions:                map[string]*appsv1alpha1.PodDecoration{},
		}
		getter.latestPodDecorations = []*appsv1alpha1.PodDecoration{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-1",
				},
				Status: appsv1alpha1.PodDecorationStatus{
					CurrentRevision: "foo-100",
					UpdatedRevision: "foo-101",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo-2",
				},
				Status: appsv1alpha1.PodDecorationStatus{
					CurrentRevision: "foo-200",
					UpdatedRevision: "foo-201",
				},
			},
		}
		getter.revisions["foo-100"] = getter.latestPodDecorations[0]
		getter.revisions["foo-101"] = getter.latestPodDecorations[0]
		getter.revisions["foo-200"] = getter.latestPodDecorations[1]
		getter.revisions["foo-201"] = getter.latestPodDecorations[1]
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{appsv1alpha1.AnnotationResourceDecorationRevision: "[{\"name\":\"foo-1\",\"revision\":\"foo-100\"},{\"name\":\"foo-2\",\"revision\":\"foo-200\"}]"}}}
		pds, err := getter.GetUpdatedDecorationsByOldPod(context.TODO(), pod)
		Expect(err).Should(BeNil())
		Expect(len(pds)).Should(Equal(2))
		Expect(pds["foo-101"]).ShouldNot(BeNil())
		Expect(pds["foo-201"]).ShouldNot(BeNil())
		getter.latestPodDecorationNames = sets.NewString()
		getter.latestPodDecorations = []*appsv1alpha1.PodDecoration{}
		pod.Annotations[appsv1alpha1.AnnotationResourceDecorationRevision] = "[{\"name\":\"foo-1\",\"revision\":\"foo-101\"},{\"name\":\"foo-2\",\"revision\":\"foo-201\"}]"
		pds, err = getter.GetUpdatedDecorationsByOldPod(context.TODO(), pod)
		Expect(err).Should(BeNil())
		Expect(len(pds)).Should(Equal(0))
	})
})
