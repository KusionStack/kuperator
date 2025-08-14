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

package gracedelete

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/kubectl/pkg/scheme"
	"kusionstack.io/kube-api/apps/v1alpha1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"kusionstack.io/kuperator/pkg/utils/feature"
)

func TestGraceDelete(t *testing.T) {
	inputs := []struct {
		keyWords string // used to check the error message
		fakePod  corev1.Pod
		oldPod   corev1.Pod

		podLabels      map[string]string
		expectedLabels map[string]string

		reqOperation admissionv1.Operation
	}{
		{
			reqOperation: admissionv1.Update,
		},
		{
			reqOperation: admissionv1.Delete,
		},
		{
			fakePod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test",
				},
			},
			oldPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test",
					Labels: map[string]string{
						fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
					},
				},
			},
			reqOperation: admissionv1.Delete,
		},
		{
			fakePod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test",
				},
			},
			oldPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test1",
					Labels: map[string]string{
						fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
					},
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{{ConditionType: v1alpha1.ReadinessGatePodServiceReady}},
				},
			},
			keyWords:     "not found",
			reqOperation: admissionv1.Delete,
		},
		{
			fakePod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test2",
					Labels: map[string]string{
						fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
						"operating.podopslifecycle.kusionstack.io/pod-delete": "1704865098763959176",
					},
				},
			},
			oldPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test2",
					Labels: map[string]string{
						fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
					},
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{{ConditionType: v1alpha1.ReadinessGatePodServiceReady}},
				},
			},
			expectedLabels: map[string]string{
				appsv1alpha1.PodDeletionIndicationLabelKey: "true",
			},
			keyWords:     "pod deletion process is underway",
			reqOperation: admissionv1.Delete,
		},
		{
			fakePod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test2",
					Labels: map[string]string{
						fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
					},
				},
			},
			oldPod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test2",
					Labels: map[string]string{
						fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
					},
					Finalizers: []string{"prot.podopslifecycle.kusionstack.io/finalizer1,prot.podopslifecycle.kusionstack.io/finalizer2"},
				},
				Spec: corev1.PodSpec{
					ReadinessGates: []corev1.PodReadinessGate{{ConditionType: v1alpha1.ReadinessGatePodServiceReady}},
				},
			},
			expectedLabels: map[string]string{
				appsv1alpha1.PodDeletionIndicationLabelKey: "true",
			},
			keyWords:     "pod deletion process is underway",
			reqOperation: admissionv1.Delete,
		},
	}

	gd := New()
	runtime.Must(feature.DefaultMutableFeatureGate.Set("GraceDeleteWebhook=true"))
	for _, v := range inputs {
		client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(&v.fakePod).Build()
		err := gd.Validating(context.Background(), client, &v.oldPod, nil, v.reqOperation)
		if v.keyWords == "" {
			assert.NoError(t, err)
		} else {
			assert.Error(t, err)
			assert.Contains(t, err.Error(), v.keyWords)
		}
		if len(v.expectedLabels) != 0 {
			pod := &corev1.Pod{}
			client.Get(context.Background(), types.NamespacedName{Namespace: v.oldPod.Namespace, Name: v.oldPod.Name}, pod)
			for k := range v.expectedLabels {
				_, exist := pod.Labels[k]
				assert.True(t, exist)
			}
		}
	}
}
