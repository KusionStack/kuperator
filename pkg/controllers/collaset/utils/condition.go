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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

const ConditionUpdatePeriodBackOff = 30 * time.Second

func AddOrUpdateCondition(status *appsv1alpha1.CollaSetStatus, conditionType appsv1alpha1.CollaSetConditionType, err error, reason, message string) {
	condStatus := corev1.ConditionTrue
	if err != nil {
		condStatus = corev1.ConditionFalse
	}

	existCond := GetCondition(status, conditionType)
	if existCond != nil {
		if existCond.Reason == reason &&
			existCond.Message == message &&
			existCond.Status == condStatus {
			now := metav1.Now()
			if now.Sub(existCond.LastTransitionTime.Time) < ConditionUpdatePeriodBackOff {
				return
			}
		}
	}

	cond := NewCondition(conditionType, condStatus, reason, message)
	SetCondition(status, cond)
}

func NewCondition(condType appsv1alpha1.CollaSetConditionType, status corev1.ConditionStatus, reason, msg string) *appsv1alpha1.CollaSetCondition {
	return &appsv1alpha1.CollaSetCondition{
		Type:               condType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            msg,
	}
}

// GetCondition returns a inplace set condition with the provided type if it exists.
func GetCondition(status *appsv1alpha1.CollaSetStatus, condType appsv1alpha1.CollaSetConditionType) *appsv1alpha1.CollaSetCondition {
	for _, c := range status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// SetCondition adds/replaces the given condition in the replicaset status. If the condition that we
// are about to add already exists and has the same status and reason then we are not going to update.
func SetCondition(status *appsv1alpha1.CollaSetStatus, condition *appsv1alpha1.CollaSetCondition) {
	currentCond := GetCondition(status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason && currentCond.LastTransitionTime == condition.LastTransitionTime {
		return
	}
	newConditions := filterOutCondition(status.Conditions, condition.Type)
	status.Conditions = append(newConditions, *condition)
}

// RemoveCondition removes the condition with the provided type from the replicaset status.
func RemoveCondition(status *appsv1alpha1.CollaSetStatus, condType appsv1alpha1.CollaSetConditionType) {
	status.Conditions = filterOutCondition(status.Conditions, condType)
}

// filterOutCondition returns a new slice of replicaset conditions without conditions with the provided type.
func filterOutCondition(conditions []appsv1alpha1.CollaSetCondition, condType appsv1alpha1.CollaSetConditionType) []appsv1alpha1.CollaSetCondition {
	var newConditions []appsv1alpha1.CollaSetCondition
	for _, c := range conditions {
		if c.Type == condType {
			continue
		}
		newConditions = append(newConditions, c)
	}
	return newConditions
}
