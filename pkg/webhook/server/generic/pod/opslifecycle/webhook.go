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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/podopslifecycle"
	"kusionstack.io/kafed/pkg/log"
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
		v1alpha1.PodPreCheckLabelPrefix:            v1alpha1.PodOperationPermissionLabelPrefix,
		v1alpha1.PodOperationPermissionLabelPrefix: v1alpha1.PodPreCheckLabelPrefix,
	}
)

type ReadyToUpgrade func(pod *corev1.Pod) (bool, []string, *time.Duration)
type TimeLabelValue func() string

type OpsLifecycle struct {
	readyToUpgrade ReadyToUpgrade

	timeLabelValue TimeLabelValue
}

func New(readyToUpgrade ReadyToUpgrade) *OpsLifecycle {
	return &OpsLifecycle{
		readyToUpgrade: readyToUpgrade,
		timeLabelValue: func() string {
			return strconv.FormatInt(time.Now().Unix(), 10)
		},
	}
}

func (lc *OpsLifecycle) Validating(ctx context.Context, pod *corev1.Pod, logger *log.Logger) error {
	expectedLabels := make(map[string]struct{})
	foundLabels := make(map[string]struct{})

	for label := range pod.Labels {
		for _, v := range pairLabelPrefixesMap { // labels must exist together and have the same id
			if !strings.HasPrefix(label, v) {
				continue
			}

			s := strings.Split(label, "/")
			if len(s) < 2 {
				return fmt.Errorf("invalid label %s", label)
			}
			id := s[1]

			if id != "" {
				label := fmt.Sprintf("%s/%s", pairLabelPrefixesMap[v], id)
				_, ok := pod.Labels[label]
				if !ok {
					return fmt.Errorf("not found label %s", label)
				}
			}
		}

		found := false
		for v := range expectedLabels {
			if strings.HasPrefix(label, v) {
				foundLabels[v] = struct{}{}
			}
			found = true
			break
		}
		if found {
			continue
		}

		for _, v := range coexistingLabelPrefixesMap { // labels must exist together
			if !strings.HasPrefix(label, v) {
				continue
			}
			expectedLabels[coexistingLabelPrefixesMap[v]] = struct{}{}
		}
	}

	if len(expectedLabels) != len(foundLabels) {
		return fmt.Errorf("not found the expected label prefixes: %v", expectedLabels)
	}
	return nil
}

func (lc *OpsLifecycle) Mutating(ctx context.Context, oldPod, newPod *corev1.Pod, _ client.Client, logger *log.Logger) error {
	lc.addReadinessGates(newPod, v1alpha1.ReadinessGatePodServiceReady)

	newIdToLabelsMap, typeToNumsMap, err := podopslifecycle.PodIDAndTypesMap(newPod)
	if err != nil {
		return err
	}

	var operatingCount, operateCount, completeCount int
	var undoTypeToNumsMap = map[string]int{}
	for id, labels := range newIdToLabelsMap {
		if undoOperationType, ok := labels[v1alpha1.PodUndoOperationTypeLabelPrefix]; ok { // operation is canceled
			if _, ok := undoTypeToNumsMap[undoOperationType]; !ok {
				undoTypeToNumsMap[undoOperationType] = 1
			} else {
				undoTypeToNumsMap[undoOperationType] = undoTypeToNumsMap[undoOperationType] + 1
			}

			// clean up these labels with id
			for _, v := range []string{v1alpha1.PodOperatingLabelPrefix, v1alpha1.PodOperationTypeLabelPrefix, v1alpha1.PodPreCheckLabelPrefix, v1alpha1.PodPreCheckedLabelPrefix, v1alpha1.PodPrepareLabelPrefix, v1alpha1.PodOperateLabelPrefix} {
				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}

			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, id))
			continue
		}

		if _, ok := labels[v1alpha1.PodOperatingLabelPrefix]; ok { // operating
			operatingCount++

			if _, ok := labels[v1alpha1.PodPreCheckedLabelPrefix]; ok {
				if _, ok := labels[v1alpha1.PodPrepareLabelPrefix]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, id))
				}
				if _, ok := labels[v1alpha1.PodOperateLabelPrefix]; !ok {
					if ready, _, _ := lc.readyToUpgrade(newPod); ready {
						lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, id))
					}
				}
			} else {
				if _, ok := labels[v1alpha1.PodPreCheckLabelPrefix]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, id))
				}
			}
		}

		if _, ok := labels[v1alpha1.PodOperateLabelPrefix]; ok {
			operateCount++
			continue
		}

		if _, ok := labels[v1alpha1.PodOperatedLabelPrefix]; ok { // operated
			if _, ok := labels[v1alpha1.PodPostCheckedLabelPrefix]; ok {
				if _, ok := labels[v1alpha1.PodCompleteLabelPrefix]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, id))
				}
			} else {
				if _, ok := labels[v1alpha1.PodPostCheckLabelPrefix]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, id))
				}
			}
		}

		if _, ok := labels[v1alpha1.PodCompleteLabelPrefix]; ok { // complete
			completeCount++
		}
	}

	for t, num := range undoTypeToNumsMap {
		if num == typeToNumsMap[t] { // reset the permission with type t if all operating with type t are canceled
			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, t))
		}
	}

	if operatingCount != 0 { // wait for all operatings to be done
		return nil
	}

	if operateCount == len(newIdToLabelsMap) { // all operations are prepared
		oldIdToLabelsMap, _, err := podopslifecycle.PodIDAndTypesMap(oldPod)
		if err != nil {
			return err
		}

		for id := range newIdToLabelsMap {
			for _, v := range []string{v1alpha1.PodPreCheckLabelPrefix, v1alpha1.PodPreCheckedLabelPrefix, v1alpha1.PodPrepareLabelPrefix, v1alpha1.PodOperateLabelPrefix} {
				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}
			lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, id))

			t, ok := oldIdToLabelsMap[id][v1alpha1.PodOperationTypeLabelPrefix]
			if !ok {
				return fmt.Errorf("pod %s/%s label %s not found", oldPod.Namespace, oldPod.Name, fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, id))
			}

			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, t))
			newPod.Labels[fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, id)] = t
		}
	}

	if completeCount == len(newIdToLabelsMap) { // all operations are done
		satisfied, _, err := lc.satisfyExpectedFinalizers(newPod)
		if err != nil {
			return err
		}
		if satisfied { // all operations are done and all expected finalizers are satisfied, then remove all unuseful labels, and add service available label
			for id := range newIdToLabelsMap {
				for _, v := range []string{v1alpha1.PodOperatedLabelPrefix, v1alpha1.PodDoneOperationTypeLabelPrefix, v1alpha1.PodPostCheckLabelPrefix, v1alpha1.PodPostCheckedLabelPrefix, v1alpha1.PodCompleteLabelPrefix} {
					delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
				}
			}
			lc.addLabelWithTime(newPod, v1alpha1.PodServiceAvailableLabel)
		}
	}

	return nil
}

func (lc *OpsLifecycle) addLabelWithTime(pod *corev1.Pod, key string) {
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[key] = lc.timeLabelValue()
}

func (lc *OpsLifecycle) addReadinessGates(pod *corev1.Pod, conditionType corev1.PodConditionType) {
	for _, v := range pod.Spec.ReadinessGates {
		if v.ConditionType == conditionType {
			return
		}
	}
	pod.Spec.ReadinessGates = append(pod.Spec.ReadinessGates, corev1.PodReadinessGate{
		ConditionType: conditionType,
	})
}

func (lc *OpsLifecycle) satisfyExpectedFinalizers(pod *corev1.Pod) (bool, []string, error) {
	satisfy := true
	var expectedFinalizer []string // expected finalizers that are not satisfied

	availableConditions, err := lc.podAvailableConditions(pod)
	if err != nil {
		return satisfy, expectedFinalizer, err
	}

	if availableConditions != nil && len(availableConditions.ExpectedFinalizers) != 0 {
		existFinalizers := sets.String{}
		for _, finalizer := range pod.Finalizers {
			existFinalizers.Insert(finalizer)
		}

		for _, finalizer := range availableConditions.ExpectedFinalizers {
			if !existFinalizers.Has(finalizer) {
				satisfy = false
				expectedFinalizer = append(expectedFinalizer, finalizer)
			}
		}
	}

	return satisfy, expectedFinalizer, nil
}

func (lc *OpsLifecycle) podAvailableConditions(pod *corev1.Pod) (*v1alpha1.PodAvailableConditions, error) {
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
