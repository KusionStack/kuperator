/*
Copyright 2026 The KusionStack Authors.

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

package rules

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/register"
)

func newCollaSet(name string, uid types.UID, replicas int32) *appsv1alpha1.CollaSet {
	return &appsv1alpha1.CollaSet{
		TypeMeta: metav1.TypeMeta{Kind: "CollaSet", APIVersion: appsv1alpha1.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			UID:       uid,
		},
		Spec: appsv1alpha1.CollaSetSpec{
			Replicas: ptr.To[int32](replicas),
		},
	}
}

func newOwnedPod(name, ownerName string, ownerUID types.UID) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: appsv1alpha1.GroupVersion.String(),
					Kind:       "CollaSet",
					Name:       ownerName,
					UID:        ownerUID,
					Controller: ptr.To(true),
				},
			},
		},
	}
}

func newRuler(objects ...runtime.Object) *AvailableRuler {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = appsv1alpha1.AddToScheme(scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
	return &AvailableRuler{
		Client: cl,
	}
}

func TestConsiderOwnerReplicas_NoOwner(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "default"}}
	r := newRuler()
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p": pod})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 for ownerless pod, got %d", got)
	}
}

func TestConsiderOwnerReplicas_NoDeficit(t *testing.T) {
	uid := types.UID("cls-uid")
	cls := newCollaSet("cls", uid, 2)
	pod1 := newOwnedPod("p1", "cls", uid)
	pod2 := newOwnedPod("p2", "cls", uid)
	r := newRuler(cls)
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p1": pod1, "p2": pod2})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 uncreated when desired==current, got %d", got)
	}
}

func TestConsiderOwnerReplicas_DeficitWhenPodMissing(t *testing.T) {
	uid := types.UID("cls-uid")
	// Simulate the PersistentSequence rebuild gap: only 1 of the 2 desired pods exists.
	cls := newCollaSet("cls", uid, 2)
	pod1 := newOwnedPod("p1", "cls", uid)
	r := newRuler(cls)
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p1": pod1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 1 {
		t.Fatalf("expected 1 uncreated (desired 2 - current 1), got %d", got)
	}
}

func TestConsiderOwnerReplicas_OwnerNotFound(t *testing.T) {
	uid := types.UID("cls-uid")
	pod1 := newOwnedPod("p1", "cls", uid)
	// No CollaSet object in client; should not error and not contribute.
	r := newRuler()
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p1": pod1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 when owner missing, got %d", got)
	}
}

func TestConsiderOwnerReplicas_UIDMismatch(t *testing.T) {
	uid := types.UID("cls-uid-old")
	cls := newCollaSet("cls", types.UID("cls-uid-new"), 2)
	pod1 := newOwnedPod("p1", "cls", uid)
	r := newRuler(cls)
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p1": pod1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 on UID mismatch, got %d", got)
	}
}

func TestConsiderOwnerReplicas_MultipleOwners(t *testing.T) {
	uidA := types.UID("a")
	uidB := types.UID("b")
	clsA := newCollaSet("cls-a", uidA, 3)
	clsB := newCollaSet("cls-b", uidB, 2)
	podA1 := newOwnedPod("a1", "cls-a", uidA)
	podB1 := newOwnedPod("b1", "cls-b", uidB)
	r := newRuler(clsA, clsB)
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"a1": podA1, "b1": podB1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// cls-a: 3-1=2; cls-b: 2-1=1; total 3
	if got != 3 {
		t.Fatalf("expected 3 uncreated across owners, got %d", got)
	}
}

func TestConsiderOwnerReplicas_NilReplicasDefaultsOne(t *testing.T) {
	uid := types.UID("cls-uid")
	cls := newCollaSet("cls", uid, 0)
	cls.Spec.Replicas = nil
	pod1 := newOwnedPod("p1", "cls", uid)
	r := newRuler(cls)
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p1": pod1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// desired defaults to 1, current 1, deficit 0
	if got != 0 {
		t.Fatalf("expected 0 when nil replicas defaults to 1, got %d", got)
	}
}

func TestConsiderOwnerReplicas_NonCollaSetOwnerIgnored(t *testing.T) {
	uid := types.UID("rs-uid")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "p",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       "rs",
					UID:        uid,
					Controller: ptr.To(true),
				},
			},
		},
	}
	r := newRuler()
	got, err := r.considerOwnerReplicas(map[string]*corev1.Pod{"p": pod})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected 0 for non-CollaSet owner, got %d", got)
	}
}

// TestFilter_PersistentSequenceRebuildGap_ProtectsWatermark reproduces the
// scenario where PersistentSequence rebuild leaves the target list with 1
// pod while the CollaSet still desires 2. Without the considerOwnerReplicas
// offset the lone remaining pod would be approved (allowing both pods to be
// unavailable simultaneously). With the offset, the deficit (1) shrinks the
// quota to 0 and the remaining subject is rejected.
func TestFilter_PersistentSequenceRebuildGap_ProtectsWatermark(t *testing.T) {
	uid := types.UID("cls-uid")
	cls := newCollaSet("cls", uid, 2)
	// Mark the surviving pod as service-available so it is "available" and
	// would otherwise consume a quota slot.
	pod1 := newOwnedPod("p1", "cls", uid)
	pod1.Labels = map[string]string{appsv1alpha1.PodServiceAvailableLabel: "true"}
	pod1.Status.Conditions = []corev1.PodCondition{
		{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
		{Type: corev1.PodReady, Status: corev1.ConditionTrue},
	}

	r := newRuler(cls)
	r.Name = "available"
	maxUnavailable := intstr.FromInt(1)
	r.MaxUnavailableValue = &maxUnavailable

	// Make processUnavailableFunc treat pods lacking the service-available
	// label as unavailable, mirroring the production podopslifecycle hook.
	prev := register.UnAvailableFuncList
	register.UnAvailableFuncList = []register.UnAvailableFunc{
		func(pod *corev1.Pod) (bool, *int64) {
			if pod.Labels == nil || pod.Labels[appsv1alpha1.PodServiceAvailableLabel] != "true" {
				return true, nil
			}
			return false, nil
		},
	}
	t.Cleanup(func() { register.UnAvailableFuncList = prev })

	ptrRule := &appsv1alpha1.PodTransitionRule{}
	targets := map[string]*corev1.Pod{"p1": pod1}
	subjects := sets.NewString("p1")

	result := r.Filter(ptrRule, targets, subjects)
	if result == nil {
		t.Fatalf("nil filter result")
	}
	if result.Passed.Has("p1") {
		t.Fatalf("p1 must be rejected: deficit should have consumed the quota, got pass=%v rejects=%v", result.Passed.List(), result.Rejected)
	}
	if _, ok := result.Rejected["p1"]; !ok {
		t.Fatalf("p1 must appear in rejects, got rejects=%v", result.Rejected)
	}
}

// TestFilter_FullFleet_NoRegression verifies that when the full fleet exists
// (desired == current), the offset is zero and the legacy behavior is
// preserved: an available subject is approved within the maxUnavailable quota.
func TestFilter_FullFleet_NoRegression(t *testing.T) {
	uid := types.UID("cls-uid")
	cls := newCollaSet("cls", uid, 2)
	pod1 := newOwnedPod("p1", "cls", uid)
	pod2 := newOwnedPod("p2", "cls", uid)
	// Both pods are service-available.
	pod1.Labels = map[string]string{appsv1alpha1.PodServiceAvailableLabel: "true"}
	pod2.Labels = map[string]string{appsv1alpha1.PodServiceAvailableLabel: "true"}
	for _, p := range []*corev1.Pod{pod1, pod2} {
		p.Status.Conditions = []corev1.PodCondition{
			{Type: corev1.ContainersReady, Status: corev1.ConditionTrue},
			{Type: corev1.PodReady, Status: corev1.ConditionTrue},
		}
	}

	r := newRuler(cls)
	r.Name = "available"
	maxUnavailable := intstr.FromInt(1)
	r.MaxUnavailableValue = &maxUnavailable

	prev := register.UnAvailableFuncList
	register.UnAvailableFuncList = []register.UnAvailableFunc{
		func(pod *corev1.Pod) (bool, *int64) {
			if pod.Labels == nil || pod.Labels[appsv1alpha1.PodServiceAvailableLabel] != "true" {
				return true, nil
			}
			return false, nil
		},
	}
	t.Cleanup(func() { register.UnAvailableFuncList = prev })

	ptrRule := &appsv1alpha1.PodTransitionRule{}
	targets := map[string]*corev1.Pod{"p1": pod1, "p2": pod2}
	// Request the transition for p1 (it is available and the quota allows 1).
	subjects := sets.NewString("p1")

	result := r.Filter(ptrRule, targets, subjects)
	if !result.Passed.Has("p1") {
		t.Fatalf("p1 should be approved when fleet is full and quota allows, got rejects=%v", result.Rejected)
	}
}
