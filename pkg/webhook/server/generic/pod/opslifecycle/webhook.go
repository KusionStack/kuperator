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
	"encoding/json"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
	controllerutils "kusionstack.io/kafed/pkg/controllers/utils"
)

const (
	waitingForLifecycleSeconds int64 = 5
)

var (
	// some labels must exist together and have the same id, and they are a pair
	pairLabelPrefixesMap = map[string]string{
		v1alpha1.PodOperatingLabelPrefix:     v1alpha1.PodOperationTypeLabelPrefix,
		v1alpha1.PodOperationTypeLabelPrefix: v1alpha1.PodOperatingLabelPrefix,

		v1alpha1.PodOperatedLabelPrefix:          v1alpha1.PodDoneOperationTypeLabelPrefix,
		v1alpha1.PodDoneOperationTypeLabelPrefix: v1alpha1.PodOperatedLabelPrefix,
	}

	// some labels must exist together
	coexistingLabelPrefixesMap = map[string]string{
		v1alpha1.PodPreCheckedLabelPrefix:          v1alpha1.PodOperationPermissionLabelPrefix,
		v1alpha1.PodOperationPermissionLabelPrefix: v1alpha1.PodPreCheckedLabelPrefix,
	}
)

type ReadyToUpgrade func(pod *corev1.Pod) (bool, []string)
type SatisfyExpectedFinalizers func(pod *corev1.Pod) (bool, []string, error)
type TimeLabelValue func() string
type IsPodReady func(pod *corev1.Pod) bool

type OpsLifecycle struct {
	readyToUpgrade            ReadyToUpgrade // for testing
	satisfyExpectedFinalizers SatisfyExpectedFinalizers
	isPodReady                IsPodReady
	timeLabelValue            TimeLabelValue
}

func New() *OpsLifecycle {
	return &OpsLifecycle{
		readyToUpgrade:            hasNoBlockingFinalizer,
		satisfyExpectedFinalizers: satisfyExpectedFinalizers,
		isPodReady:                controllerutils.IsPodReady,
		timeLabelValue: func() string {
			return strconv.FormatInt(time.Now().Unix(), 10)
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

func (lc *OpsLifecycle) podServiceAvailableLabel(pod *corev1.Pod) error {
	if _, ok := pod.Labels[v1alpha1.PodServiceAvailableLabel]; ok {
		return nil
	}

	satisfied, expectedFinalizer, err := lc.satisfyExpectedFinalizers(pod) // whether all expected finalizers are satisfied
	if err != nil || !satisfied {
		klog.Infof("pod: %s/%s, expected finalizers: %v, err: %v", pod.Namespace, pod.Name, expectedFinalizer, err)
		return err
	}

	if !lc.isPodReady(pod) {
		return nil
	}

	lc.addLabelWithTime(pod, v1alpha1.PodServiceAvailableLabel)
	return nil
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

func satisfyExpectedFinalizers(pod *corev1.Pod) (bool, []string, error) {
	satisfied := true
	var expectedFinalizer []string // expected finalizers that are not satisfied

	availableConditions, err := podAvailableConditions(pod)
	if err != nil {
		return satisfied, expectedFinalizer, err
	}

	if availableConditions != nil && len(availableConditions.ExpectedFinalizers) != 0 {
		existFinalizers := sets.String{}
		for _, finalizer := range pod.Finalizers {
			existFinalizers.Insert(finalizer)
		}

		for _, finalizer := range availableConditions.ExpectedFinalizers {
			if !existFinalizers.Has(finalizer) {
				satisfied = false
				expectedFinalizer = append(expectedFinalizer, finalizer)
			}
		}
	}

	return satisfied, expectedFinalizer, nil
}

func hasNoBlockingFinalizer(pod *corev1.Pod) (bool, []string) {
	if pod == nil {
		return true, nil
	}

	hasReadinessGate := false
	if pod.Spec.ReadinessGates != nil {
		for _, readinessGate := range pod.Spec.ReadinessGates {
			if readinessGate.ConditionType == v1alpha1.ReadinessGatePodServiceReady {
				hasReadinessGate = true
				break
			}
		}
	}
	if !hasReadinessGate {
		// if has no service-ready ReadinessGate, treat it as normal pod.
		return true, nil
	}

	if pod.ObjectMeta.Finalizers == nil || len(pod.ObjectMeta.Finalizers) == 0 {
		return true, nil
	}

	var finalizers []string
	for _, f := range pod.ObjectMeta.Finalizers {
		if strings.HasPrefix(f, v1alpha1.PodOperationProtectionFinalizerPrefix) {
			finalizers = append(finalizers, f)
		}
	}

	if len(finalizers) > 0 {
		return false, finalizers
	}

	return true, nil
}

func podAvailableConditions(pod *corev1.Pod) (*v1alpha1.PodAvailableConditions, error) {
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
