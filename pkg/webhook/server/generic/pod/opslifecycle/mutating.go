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

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/podopslifecycle"
	"kusionstack.io/kafed/pkg/utils"
)

func (lc *OpsLifecycle) Mutating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	if !utils.ControlledByKafed(newPod) || operation != admissionv1.Update {
		return nil
	}
	addReadinessGates(newPod, v1alpha1.ReadinessGatePodServiceReady)

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
