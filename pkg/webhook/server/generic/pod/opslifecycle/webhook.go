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
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/log"
	"kusionstack.io/kafed/pkg/utils"
)

var (
	pairLabelsMap       = map[string]string{}
	coexistingLabelsMap = map[string]string{}
)

func init() {
	pairLabels := []struct {
		label1 string
		label2 string
	}{
		{v1alpha1.LabelPodOperating, v1alpha1.LabelPodOperationType},
		{v1alpha1.LabelPodOperated, v1alpha1.LabelPodDoneOperationType},
	}
	for _, v := range pairLabels {
		pairLabelsMap[v.label1] = v.label2
		pairLabelsMap[v.label2] = v.label1
	}

	coexistingLabels := []struct {
		label1 string
		label2 string
	}{
		{v1alpha1.LabelPodPreCheck, v1alpha1.LabelPodOperationPermission},
	}
	for _, v := range coexistingLabels {
		coexistingLabelsMap[v.label1] = v.label2
		coexistingLabelsMap[v.label2] = v.label1
	}
}

type ReadyToUpgrade func(pod *corev1.Pod) (bool, []string, *time.Duration)
type SatisfyExpectedFinalizers func(pod *corev1.Pod) (bool, []string, error)
type TimeLabelValue func() string

type OpsLifecycle struct {
	readyToUpgrade            ReadyToUpgrade
	satisfyExpectedFinalizers SatisfyExpectedFinalizers

	timeLabelValue TimeLabelValue
}

func New(readyToUpgrade ReadyToUpgrade, satisfyExpectedFinalizers SatisfyExpectedFinalizers) *OpsLifecycle {
	return &OpsLifecycle{
		readyToUpgrade:            readyToUpgrade,
		satisfyExpectedFinalizers: satisfyExpectedFinalizers,

		timeLabelValue: func() string {
			return strconv.FormatInt(time.Now().Unix(), 10)
		},
	}
}

func (lc *OpsLifecycle) Validating(ctx context.Context, pod *corev1.Pod, logger *log.Logger) error {
	expectedLabels := make(map[string]struct{})

	for label := range pod.Labels {
		for _, v := range pairLabelsMap { // labels must exist together and have the same id
			if !strings.HasPrefix(label, v) {
				continue
			}

			s := strings.Split(label, "/")
			if len(s) < 2 {
				return fmt.Errorf("invalid label %s", label)
			}
			id := s[1]

			if id != "" {
				label := fmt.Sprintf("%s/%s", pairLabelsMap[v], id)
				_, ok := pod.Labels[label]
				if !ok {
					return fmt.Errorf("not found label %s", label)
				}
			}
		}

		expected := false
		for v := range expectedLabels {
			if strings.HasPrefix(label, v) {
				delete(expectedLabels, v)
			}
			expected = true
			break
		}
		if expected {
			continue
		}

		for _, v := range coexistingLabelsMap { // labels must exist together
			if !strings.HasPrefix(label, v) {
				continue
			}
			expectedLabels[coexistingLabelsMap[v]] = struct{}{}
		}
	}

	if len(expectedLabels) > 0 {
		return fmt.Errorf("not found labels %v", expectedLabels)
	}
	return nil
}

func (lc *OpsLifecycle) Mutating(ctx context.Context, oldPod, newPod *corev1.Pod, _ client.Client, logger *log.Logger) error {
	newIdToLabelsMap, typeToNumsMap, err := utils.IDAndTypesMap(newPod)
	if err != nil {
		return err
	}

	var operatingCount, operateCount, completeCount int
	var undoTypeToNumsMap = map[string]int{}
	for id, labels := range newIdToLabelsMap {
		if undoOperationType, ok := labels[v1alpha1.LabelPodUndoOperationType]; ok {
			if _, ok := undoTypeToNumsMap[undoOperationType]; !ok {
				undoTypeToNumsMap[undoOperationType] = 1
			} else {
				undoTypeToNumsMap[undoOperationType] = undoTypeToNumsMap[undoOperationType] + 1
			}

			// clean up these labels with id
			for _, v := range []string{v1alpha1.LabelPodOperating, v1alpha1.LabelPodOperationType, v1alpha1.LabelPodPreCheck, v1alpha1.LabelPodPreChecked, v1alpha1.LabelPodPrepare, v1alpha1.LabelPodOperate} {
				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}

			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.LabelPodUndoOperationType, id))
			continue
		}

		if _, ok := labels[v1alpha1.LabelPodOperating]; ok {
			operatingCount++

			if _, ok := labels[v1alpha1.LabelPodPreChecked]; ok {
				if _, ok := labels[v1alpha1.LabelPodPrepare]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, id))
				}
				if _, ok := labels[v1alpha1.LabelPodOperate]; !ok {
					if ready, _, _ := lc.readyToUpgrade(newPod); ready {
						lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperate, id))
					}
				}
			} else {
				if _, ok := labels[v1alpha1.LabelPodPreCheck]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, id))
				}
			}
		}

		if _, ok := labels[v1alpha1.LabelPodOperate]; ok {
			operateCount++
			continue
		}

		if _, ok := labels[v1alpha1.LabelPodOperated]; ok {
			if _, ok := labels[v1alpha1.LabelPodPostChecked]; ok {
				if _, ok := labels[v1alpha1.LabelPodComplete]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, id))
				}
			} else {
				if _, ok := labels[v1alpha1.LabelPodPostCheck]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, id))
				}
			}
		}

		if _, ok := labels[v1alpha1.LabelPodComplete]; ok {
			completeCount++
		}
	}

	for t, num := range undoTypeToNumsMap {
		if num == typeToNumsMap[t] { // reset the permission with type t if all operating with type t are canceled
			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, t))
		}
	}

	if operatingCount != 0 { // wait for all operatings to be done
		return nil
	}

	if operateCount == len(newIdToLabelsMap) {
		oldIdToLabelsMap, _, err := utils.IDAndTypesMap(oldPod)
		if err != nil {
			return err
		}

		for id := range newIdToLabelsMap {
			for _, v := range []string{v1alpha1.LabelPodPreCheck, v1alpha1.LabelPodPreChecked, v1alpha1.LabelPodPrepare, v1alpha1.LabelPodOperate} {
				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}
			lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, id))

			t, ok := oldIdToLabelsMap[id][v1alpha1.LabelPodOperationType]
			if !ok {
				return fmt.Errorf("pod %s/%s label %s not found", oldPod.Namespace, oldPod.Name, fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, id))
			}

			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, t))
			newPod.Labels[fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, id)] = t
		}
	}

	if completeCount == len(newIdToLabelsMap) {
		satisfied, _, err := lc.satisfyExpectedFinalizers(newPod)
		if err != nil {
			return err
		}
		if satisfied {
			for id := range newIdToLabelsMap {
				for _, v := range []string{v1alpha1.LabelPodOperated, v1alpha1.LabelPodDoneOperationType, v1alpha1.LabelPodPostCheck, v1alpha1.LabelPodPostChecked, v1alpha1.LabelPodComplete} {
					delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
				}
			}
		}
	}

	return nil
}

func (lc *OpsLifecycle) addLabelWithTime(newPod *corev1.Pod, key string) {
	if newPod.Labels == nil {
		newPod.Labels = make(map[string]string)
	}
	newPod.Labels[key] = lc.timeLabelValue()
}
