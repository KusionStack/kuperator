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
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

func TestIsPodScheduled(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
	}

	if IsPodScheduled(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{},
		},
	}

	if IsPodScheduled(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	if IsPodScheduled(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	if !IsPodScheduled(pod) {
		t.Fatalf("expected failure")
	}
}

func TestIsPodReady(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
	}

	if IsPodReady(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	if IsPodReady(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	if !IsPodReady(pod) {
		t.Fatalf("expected failure")
	}
}

func TestIsPodTerminal(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodFailed,
		},
	}

	if !IsPodTerminal(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodSucceeded,
		},
	}

	if !IsPodTerminal(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
		},
	}

	if IsPodTerminal(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	if IsPodTerminal(pod) {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
		Status: corev1.PodStatus{
			Phase: corev1.PodUnknown,
		},
	}

	if IsPodTerminal(pod) {
		t.Fatalf("expected failure")
	}
}

func TestIsPodServiceAvailable(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
	}

	if IsPodServiceAvailable(pod) {
		t.Fatalf("expected failure")
	}

	pod.Labels = map[string]string{
		appsv1alpha1.PodServiceAvailableLabel: "true",
	}

	if !IsPodServiceAvailable(pod) {
		t.Fatalf("expected failure")
	}
}

func TestGetProtectionFinalizers(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
	}

	if finalizers := GetProtectionFinalizers(pod); finalizers != nil {
		t.Fatalf("expected failure")
	}

	protectionFinalizer := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperationProtectionFinalizerPrefix, "finalizer1")
	pod.ObjectMeta.Finalizers = []string{
		protectionFinalizer,
		"finalizer2",
	}

	finalizers := GetProtectionFinalizers(pod)
	if len(finalizers) != 1 || finalizers[0] != protectionFinalizer {
		t.Fatalf("expected failure")
	}
}

func TestIsPodExpectedFinalizerSatisfied(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: corev1.PodSpec{},
	}

	if satisfied, _, err := IsExpectedFinalizerSatisfied(pod); err != nil || !satisfied {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test",
			Annotations: map[string]string{},
		},
		Spec: corev1.PodSpec{},
	}

	if satisfied, _, err := IsExpectedFinalizerSatisfied(pod); err != nil || !satisfied {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				appsv1alpha1.PodAvailableConditionsAnnotation: "invalid",
			},
		},
		Spec: corev1.PodSpec{},
	}

	if _, _, err := IsExpectedFinalizerSatisfied(pod); err == nil {
		t.Fatalf("expected failure")
	}

	condFinalizer1 := "test/cond1"
	condFinalizer2 := "test/cond2"
	cond := &appsv1alpha1.PodAvailableConditions{
		ExpectedFinalizers: map[string]string{
			"cond1": condFinalizer1,
			"cond2": condFinalizer2,
		},
	}
	s, err := json.Marshal(cond)
	if err != nil {
		t.Fatalf("unexpected err")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				appsv1alpha1.PodAvailableConditionsAnnotation: string(s),
			},
		},
		Spec: corev1.PodSpec{},
	}

	if satisfied, _, err := IsExpectedFinalizerSatisfied(pod); err != nil || satisfied {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				appsv1alpha1.PodAvailableConditionsAnnotation: string(s),
			},
			Finalizers: []string{
				condFinalizer1,
			},
		},
		Spec: corev1.PodSpec{},
	}

	if satisfied, _, err := IsExpectedFinalizerSatisfied(pod); err != nil || satisfied {
		t.Fatalf("expected failure")
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				appsv1alpha1.PodAvailableConditionsAnnotation: string(s),
			},
			Finalizers: []string{
				condFinalizer1,
				condFinalizer2,
			},
		},
		Spec: corev1.PodSpec{},
	}

	if satisfied, _, err := IsExpectedFinalizerSatisfied(pod); err != nil || !satisfied {
		t.Fatalf("expected failure")
	}
}
