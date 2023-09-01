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

package collaset

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

type testCase struct {
	cls             *appsv1alpha1.CollaSet
	old             *appsv1alpha1.CollaSet
	messageKeyWords string
}

func TestValidatingCollaSet(t *testing.T) {
	successCases := []testCase{
		{
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
				},
			},
		},
	}

	validatingHandler := NewValidatingHandler()
	mutatingHandler := NewMutatingHandler()

	for i, tc := range successCases {
		mutatingHandler.setDetaultCollaSet(tc.cls)
		if err := validatingHandler.validate(tc.cls, tc.old); err != nil {
			t.Fatalf("got unexpected err for %d case: %s", i, err)
		}
	}

	failureCases := map[string]testCase{
		"invalid-replicas": {
			messageKeyWords: "replicas should not be smaller than 0",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(-1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
				},
			},
		},
		"empty-selector": {
			messageKeyWords: "selector is required",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
				},
			},
		},
		"labels-not-provided": {
			messageKeyWords: "labels is required",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
				},
			},
		},
		"selector-not-match": {
			messageKeyWords: "selector does not match labels in pod template",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "not-match",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
				},
			},
		},
		"invalid-update-policy": {
			messageKeyWords: "supported values: \"ReCreate\", \"InPlaceIfPossible\", \"InPlaceOnly\"",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
					UpdateStrategy: appsv1alpha1.UpdateStrategy{
						PodUpdatePolicy: appsv1alpha1.PodUpdateStrategyType("no-exist"),
					},
				},
			},
		},
		"invalid-partition": {
			messageKeyWords: "partition should not be smaller than 0",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
					UpdateStrategy: appsv1alpha1.UpdateStrategy{
						RollingUpdate: &appsv1alpha1.RollingUpdateCollaSetStrategy{
							ByPartition: &appsv1alpha1.ByPartition{
								Partition: int32Pointer(-1),
							},
						},
					},
				},
			},
		},
		"invalid-scale-operation-delay-seconds": {
			messageKeyWords: "operationDelaySeconds should not be smaller than 0",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
					ScaleStrategy: appsv1alpha1.ScaleStrategy{
						OperationDelaySeconds: int32Pointer(-1),
					},
				},
			},
		},
		"invalid-update-operation-delay-seconds": {
			messageKeyWords: "operationDelaySeconds should not be smaller than 0",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
					UpdateStrategy: appsv1alpha1.UpdateStrategy{
						OperationDelaySeconds: int32Pointer(-1),
					},
				},
			},
		},
		"context-change-forbidden": {
			messageKeyWords: "scaleStrategy.context is not allowed to be changed",
			cls: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
					ScaleStrategy: appsv1alpha1.ScaleStrategy{
						Context: "context",
					},
				},
			},
			old: &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "foo",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "foo",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "foo",
									Image: "image:v1",
								},
							},
						},
					},
					ScaleStrategy: appsv1alpha1.ScaleStrategy{
						Context: "context-old",
					},
				},
			},
		},
	}

	for key, tc := range failureCases {
		mutatingHandler.setDetaultCollaSet(tc.cls)
		err := validatingHandler.validate(tc.cls, tc.old)
		if err == nil {
			t.Fatalf("expected err, got nil in case %s", key)
		}
		if !strings.Contains(err.Error(), tc.messageKeyWords) {
			t.Fatalf("can not find message key words [%s] in case %s, got %s", tc.messageKeyWords, key, err)
		}
	}
}

func int32Pointer(val int32) *int32 {
	return &val
}
