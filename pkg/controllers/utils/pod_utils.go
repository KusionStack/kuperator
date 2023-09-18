/*
Copyright 2014 The Kubernetes Authors.
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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	"kusionstack.io/operating/apis/apps/v1alpha1"
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	revisionutils "kusionstack.io/operating/pkg/controllers/utils/revision"
)

func GetPodRevisionPatch(revision *appsv1.ControllerRevision) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(revision.Data.Raw), &raw); err != nil {
		return nil, err
	}

	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	patch, err := json.Marshal(template)
	return patch, err
}

func ApplyPatchFromRevision(pod *corev1.Pod, revision *appsv1.ControllerRevision) (*corev1.Pod, error) {
	patch, err := GetPodRevisionPatch(revision)
	if err != nil {
		return nil, err
	}

	clone := pod.DeepCopy()
	patched, err := strategicpatch.StrategicMergePatch([]byte(runtime.EncodeOrDie(revisionutils.PodCodec, clone)), patch, clone)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(patched, clone)
	if err != nil {
		return nil, err
	}
	return clone, nil
}

// PatchToPod Use three way merge to get a updated pod.
func PatchToPod(currentRevisionPod, updateRevisionPod, currentPod *corev1.Pod) (*corev1.Pod, error) {
	currentRevisionPodBytes, err := json.Marshal(currentRevisionPod)
	if err != nil {
		return nil, err
	}
	updateRevisionPodBytes, err := json.Marshal(updateRevisionPod)

	if err != nil {
		return nil, err
	}

	// 1. find the extra changes based on current revision
	patch, err := strategicpatch.CreateTwoWayMergePatch(currentRevisionPodBytes, updateRevisionPodBytes, &corev1.Pod{})
	if err != nil {
		return nil, err
	}

	// 2. apply above changes to current pod
	// We don't apply the diff between currentPod and currentRevisionPod to updateRevisionPod,
	// because the PodTemplate changes should have the highest priority.
	currentPodBytes, err := json.Marshal(currentPod)
	if err != nil {
		return nil, err
	}
	if updateRevisionPodBytes, err = strategicpatch.StrategicMergePatch(currentPodBytes, patch, &corev1.Pod{}); err != nil {
		return nil, err
	}

	newPod := &corev1.Pod{}
	err = json.Unmarshal(updateRevisionPodBytes, newPod)
	return newPod, err
}

func NewPodFrom(owner metav1.Object, ownerRef *metav1.OwnerReference, revision *appsv1.ControllerRevision) (*corev1.Pod, error) {
	pod, err := GetPodFromRevision(revision)
	if err != nil {
		return pod, err
	}

	pod.Namespace = owner.GetNamespace()
	pod.GenerateName = GetPodsPrefix(owner.GetName())
	pod.OwnerReferences = append(pod.OwnerReferences, *ownerRef)

	pod.Labels[appsv1.ControllerRevisionHashLabelKey] = revision.Name

	return pod, nil
}

func GetPodFromRevision(revision *appsv1.ControllerRevision) (*corev1.Pod, error) {
	pod, err := ApplyPatchFromRevision(&corev1.Pod{}, revision)
	if err != nil {
		return nil, err
	}

	return pod, nil
}

func GetPodsPrefix(controllerName string) string {
	// use the dash (if the name isn't too long) to make the pod name a bit prettier
	prefix := fmt.Sprintf("%s-", controllerName)
	if len(apimachineryvalidation.NameIsDNSSubdomain(prefix, true)) != 0 {
		prefix = controllerName
	}
	return prefix
}

func ComparePod(l, r *corev1.Pod) bool {
	// 1. Unassigned < assigned
	// If only one of the pods is unassigned, the unassigned one is smaller
	if l.Spec.NodeName != r.Spec.NodeName && (len(l.Spec.NodeName) == 0 || len(r.Spec.NodeName) == 0) {
		return len(l.Spec.NodeName) == 0
	}
	// 2. PodPending < PodUnknown < PodRunning
	m := map[corev1.PodPhase]int{corev1.PodPending: 0, corev1.PodUnknown: 1, corev1.PodRunning: 2}
	if m[l.Status.Phase] != m[r.Status.Phase] {
		return m[l.Status.Phase] < m[r.Status.Phase]
	}
	// 3. Not ready < ready
	// If only one of the pods is not ready, the not ready one is smaller
	if IsPodReady(l) != IsPodReady(r) {
		return !IsPodReady(l)
	}
	// TODO: take availability into account when we push minReadySeconds information from deployment into pods,
	//       see https://github.com/kubernetes/kubernetes/issues/22065
	// 4. Been ready for empty time < less time < more time
	// If both pods are ready, the latest ready one is smaller
	if IsPodReady(l) && IsPodReady(r) && !podReadyTime(l).Equal(podReadyTime(r)) {
		return afterOrZero(podReadyTime(l), podReadyTime(r))
	}
	// 5. Pods with containers with higher restart counts < lower restart counts
	if maxContainerRestarts(l) != maxContainerRestarts(r) {
		return maxContainerRestarts(l) > maxContainerRestarts(r)
	}
	// 6. Empty creation time pods < newer pods < older pods
	if !l.CreationTimestamp.Equal(&r.CreationTimestamp) {
		return afterOrZero(&l.CreationTimestamp, &r.CreationTimestamp)
	}
	return false
}

func maxContainerRestarts(pod *corev1.Pod) int {
	var maxRestarts int32
	for _, c := range pod.Status.ContainerStatuses {
		if c.RestartCount > maxRestarts {
			maxRestarts = c.RestartCount
		}
	}
	return int(maxRestarts)
}

func podReadyTime(pod *corev1.Pod) *metav1.Time {
	if IsPodReady(pod) {
		for _, c := range pod.Status.Conditions {
			// we only care about pod ready conditions
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return &c.LastTransitionTime
			}
		}
	}
	return &metav1.Time{}
}

// afterOrZero checks if time t1 is after time t2; if one of them
// is zero, the zero time is seen as after non-zero time.
func afterOrZero(t1, t2 *metav1.Time) bool {
	if t1.Time.IsZero() || t2.Time.IsZero() {
		return t1.Time.IsZero()
	}
	return t1.After(t2.Time)
}

func IsPodScheduled(pod *corev1.Pod) bool {
	return IsPodScheduledConditionTrue(pod.Status)
}

func IsPodScheduledConditionTrue(status corev1.PodStatus) bool {
	condition := GetPodScheduledCondition(status)
	return condition != nil && condition.Status == corev1.ConditionTrue
}

func GetPodScheduledCondition(status corev1.PodStatus) *corev1.PodCondition {
	_, condition := GetPodCondition(&status, corev1.PodScheduled)
	return condition
}

// IsPodReady returns true if a pod is ready; false otherwise.
func IsPodReady(pod *corev1.Pod) bool {
	return IsPodReadyConditionTrue(pod.Status)
}

// IsPodTerminal returns true if a pod is terminal, all containers are stopped and cannot ever regress.
func IsPodTerminal(pod *corev1.Pod) bool {
	return IsPodPhaseTerminal(pod.Status.Phase)
}

// IsPodPhaseTerminal returns true if the pod's phase is terminal.
func IsPodPhaseTerminal(phase corev1.PodPhase) bool {
	return phase == corev1.PodFailed || phase == corev1.PodSucceeded
}

// IsPodReadyConditionTrue returns true if a pod is ready; false otherwise.
func IsPodReadyConditionTrue(status corev1.PodStatus) bool {
	condition := GetPodReadyCondition(status)
	return condition != nil && condition.Status == corev1.ConditionTrue
}

// GetPodReadyCondition extracts the pod ready condition from the given status and returns that.
// Returns nil if the condition is not present.
func GetPodReadyCondition(status corev1.PodStatus) *corev1.PodCondition {
	_, condition := GetPodCondition(&status, corev1.PodReady)
	return condition
}

// GetPodCondition extracts the provided condition from the given status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
func GetPodCondition(status *corev1.PodStatus, conditionType corev1.PodConditionType) (int, *corev1.PodCondition) {
	if status == nil {
		return -1, nil
	}
	return GetPodConditionFromList(status.Conditions, conditionType)
}

// GetPodConditionFromList extracts the provided condition from the given list of condition and
// returns the index of the condition and the condition. Returns -1 and nil if the condition is not present.
func GetPodConditionFromList(conditions []corev1.PodCondition, conditionType corev1.PodConditionType) (int, *corev1.PodCondition) {
	if conditions == nil {
		return -1, nil
	}
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return i, &conditions[i]
		}
	}
	return -1, nil
}

func IsServiceAvailable(pod *corev1.Pod) bool {
	if pod.Labels == nil {
		return false
	}

	_, exist := pod.Labels[appsv1alpha1.PodServiceAvailableLabel]
	return exist
}

func IsPodUpdatedRevision(pod *corev1.Pod, revision string) bool {
	if pod.Labels == nil {
		return false
	}

	return pod.Labels[appsv1.ControllerRevisionHashLabelKey] == revision
}

func SatisfyExpectedFinalizers(pod *corev1.Pod) (bool, []string, error) {
	satisfied := true
	var expectedFinalizers []string // expected finalizers that are not satisfied

	availableConditions, err := PodAvailableConditions(pod)
	if err != nil {
		return satisfied, expectedFinalizers, err
	}

	if availableConditions != nil && len(availableConditions.ExpectedFinalizers) != 0 {
		existFinalizers := sets.String{}
		for _, finalizer := range pod.Finalizers {
			existFinalizers.Insert(finalizer)
		}

		for _, finalizer := range availableConditions.ExpectedFinalizers {
			if !existFinalizers.Has(finalizer) {
				satisfied = false
				expectedFinalizers = append(expectedFinalizers, finalizer)
			}
		}
	}

	return satisfied, expectedFinalizers, nil
}

func PodAvailableConditions(pod *corev1.Pod) (*v1alpha1.PodAvailableConditions, error) {
	if pod.Annotations == nil {
		return nil, nil
	}

	anno, ok := pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation]
	if !ok {
		return nil, nil
	}

	availableConditions := &v1alpha1.PodAvailableConditions{}
	if err := json.Unmarshal([]byte(anno), availableConditions); err != nil {
		return nil, err
	}
	return availableConditions, nil
}
