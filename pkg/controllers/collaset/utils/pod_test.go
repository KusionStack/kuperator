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
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

var _ = Describe("Pod utils", func() {
	It("test get pod instanceID", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					appsv1alpha1.PodInstanceIDLabelKey: "0",
				},
			},
		}
		pws := []*PodWrapper{
			{
				Pod: &corev1.Pod{},
				ID:  0,
			},
			{
				Pod: &corev1.Pod{},
				ID:  1,
			},
		}
		Expect(len(CollectPodInstanceID(pws))).Should(Equal(2))
		id, err := GetPodInstanceID(pod)
		Expect(id).Should(Equal(0))
		Expect(err).Should(BeNil())
		pod.Labels = map[string]string{}
		_, err = GetPodInstanceID(pod)
		Expect(err).ShouldNot(BeNil())
		pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = "xxx"
		_, err = GetPodInstanceID(pod)
		Expect(err).ShouldNot(BeNil())
	})
	It("test NewPodFrom", func() {
		data := map[string]interface{}{
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"$patch": "replace",
					"metadata": map[string]interface{}{
						"labels": map[string]string{
							"foo": "bar",
						},
					},
				},
			},
		}
		raw, _ := json.Marshal(data)
		pod, err := NewPodFrom(
			&appsv1alpha1.CollaSet{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"}},
			&metav1.OwnerReference{
				Name: "foo",
			},
			&appsv1.ControllerRevision{
				Data: runtime.RawExtension{
					Raw: raw,
				},
			})
		Expect(err).Should(BeNil())
		Expect(pod.Labels["foo"]).Should(Equal("bar"))
		_, err = NewPodFrom(
			&appsv1alpha1.CollaSet{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"}},
			&metav1.OwnerReference{
				Name: "foo",
			},
			&appsv1.ControllerRevision{
				Data: runtime.RawExtension{
					Raw: []byte("x"),
				},
			})
		Expect(err).ShouldNot(BeNil())
	})
	It("test patch pods", func() {
		currentRevisionPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"foo": "bar"}},
		}
		updateRevisionPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"foo": "bar-1"}},
		}
		currentPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"foo": "bar"}},
		}
		pod, err := PatchToPod(currentRevisionPod, updateRevisionPod, currentPod)
		Expect(err).Should(BeNil())
		Expect(pod.Labels["foo"]).Should(Equal("bar-1"))
	})
})
