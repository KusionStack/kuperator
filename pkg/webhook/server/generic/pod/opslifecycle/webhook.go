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

package opslifecycle

import (
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"

	"kusionstack.io/kube-api/apps/v1alpha1"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
)

var (
	// Some labels must exist together and have the same ID
	pairLabelPrefixesMap = map[string]string{
		v1alpha1.PodOperatedLabelPrefix:          v1alpha1.PodDoneOperationTypeLabelPrefix,
		v1alpha1.PodDoneOperationTypeLabelPrefix: v1alpha1.PodOperatedLabelPrefix,
	}

	// Some labels must exist together
	coexistingLabelPrefixesMap = map[string]string{
		v1alpha1.PodPreCheckedLabelPrefix:          v1alpha1.PodOperationPermissionLabelPrefix,
		v1alpha1.PodOperationPermissionLabelPrefix: v1alpha1.PodPreCheckedLabelPrefix,
	}
)

type ReadyToOperate func(pod *corev1.Pod) bool
type TimeLabelValue func() string

type OpsLifecycle struct {
	readyToOperate ReadyToOperate
	timeLabelValue TimeLabelValue
}

func New() *OpsLifecycle {
	return &OpsLifecycle{
		readyToOperate: readyToOperate,
		timeLabelValue: func() string {
			return strconv.FormatInt(time.Now().UnixNano(), 10)
		},
	}
}

func (lc *OpsLifecycle) Name() string {
	return "PodOpsLifecycleWebhook"
}

func (lc *OpsLifecycle) addLabelWithTime(pod *corev1.Pod, key string) {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[key] = lc.timeLabelValue()
}

func addReadinessGates(pod *corev1.Pod, conditionType corev1.PodConditionType) {
	for _, v := range pod.Spec.ReadinessGates {
		if v.ConditionType == conditionType {
			return
		}
	}
	pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{
		ConditionType: conditionType,
	})
}

func readyToOperate(pod *corev1.Pod) bool {
	if pod == nil {
		return false
	}

	index, condition := controllerutils.GetPodCondition(&pod.Status, v1alpha1.ReadinessGatePodServiceReady)
	if index == -1 {
		return true
	}

	finalizers := controllerutils.GetProtectionFinalizers(pod)
	if len(finalizers) > 0 {
		return false
	}

	if condition.Status == corev1.ConditionFalse {
		return true
	}
	return false
}
