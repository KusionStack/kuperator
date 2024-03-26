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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"kusionstack.io/operating/apis/apps/v1alpha1"
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func BeforeReady(po *corev1.Pod) bool {
	return len(po.Spec.NodeName) == 0 ||
		po.Status.Phase != corev1.PodRunning ||
		!IsPodReady(po)
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

func IsPodServiceAvailable(pod *corev1.Pod) bool {
	if pod.Labels == nil {
		return false
	}

	_, exist := pod.Labels[appsv1alpha1.PodServiceAvailableLabel]
	return exist
}

func GetProtectionFinalizers(pod *corev1.Pod) []string {
	if pod == nil || pod.ObjectMeta.Finalizers == nil || len(pod.ObjectMeta.Finalizers) == 0 {
		return nil
	}

	var finalizers []string
	for _, f := range pod.ObjectMeta.Finalizers {
		if strings.HasPrefix(f, v1alpha1.PodOperationProtectionFinalizerPrefix) {
			finalizers = append(finalizers, f)
		}
	}
	return finalizers
}

func IsExpectedFinalizerSatisfied(pod *corev1.Pod) (bool, map[string]string, error) {
	satisfied := true
	notSatisfiedFinalizers := make(map[string]string) // expected finalizers that are not satisfied

	availableConditions, err := PodAvailableConditions(pod)
	if err != nil {
		return true, notSatisfiedFinalizers, err
	}

	if availableConditions != nil && len(availableConditions.ExpectedFinalizers) != 0 {
		existFinalizers := sets.String{}
		for _, finalizer := range pod.Finalizers {
			existFinalizers.Insert(finalizer)
		}

		// Check if all expected finalizers are satisfied
		for expectedFlzKey, finalizer := range availableConditions.ExpectedFinalizers {
			if !existFinalizers.Has(finalizer) {
				satisfied = false
				notSatisfiedFinalizers[expectedFlzKey] = finalizer
			}
		}
	}

	return satisfied, notSatisfiedFinalizers, nil
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
