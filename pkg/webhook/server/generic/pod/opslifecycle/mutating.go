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
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/podopslifecycle"
	"kusionstack.io/kafed/pkg/utils"
)

func (lc *OpsLifecycle) Mutating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	if !utils.ControlledByPodOpsLifecycle(newPod) {
		return nil
	}

	// add readiness gate when pod is created
	if operation == admissionv1.Create {
		addReadinessGates(newPod, v1alpha1.ReadinessGatePodServiceReady)
	}

	newIDToLabelsMap, typeToNumsMap, err := podopslifecycle.PodIDAndTypesMap(newPod)
	if err != nil {
		return err
	}
	numOfIDs := len(newIDToLabelsMap)

	var operatingCount, operateCount, operatedCount, completeCount int
	var undoTypeToNumsMap = map[string]int{}
	for id, labels := range newIDToLabelsMap {
		if undoOperationType, ok := labels[v1alpha1.PodUndoOperationTypeLabelPrefix]; ok { // operation is canceled
			if _, ok := undoTypeToNumsMap[undoOperationType]; !ok {
				undoTypeToNumsMap[undoOperationType] = 1
			} else {
				undoTypeToNumsMap[undoOperationType] = undoTypeToNumsMap[undoOperationType] + 1
			}

			// clean up these labels with id
			for _, v := range []string{v1alpha1.PodOperatingLabelPrefix,
				v1alpha1.PodOperationTypeLabelPrefix,
				v1alpha1.PodPreCheckLabelPrefix,
				v1alpha1.PodPreCheckedLabelPrefix,
				v1alpha1.PodPrepareLabelPrefix,
				v1alpha1.PodOperateLabelPrefix} {

				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}

			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, id))
			continue
		}

		if _, ok := labels[v1alpha1.PodOperatingLabelPrefix]; ok { // operating
			operatingCount++

			if _, ok := labels[v1alpha1.PodPreCheckedLabelPrefix]; ok { // pre-checked
				if _, ok := labels[v1alpha1.PodPrepareLabelPrefix]; !ok {
					delete(newPod.Labels, v1alpha1.PodServiceAvailableLabel)

					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, id)) // prepare
				} else if _, ok := labels[v1alpha1.PodOperateLabelPrefix]; !ok {
					if ready, _ := lc.readyToUpgrade(newPod); ready {
						delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, id))

						lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, id)) // operate, controllers can begin to operate
					}
				}
			} else {
				if _, ok := labels[v1alpha1.PodPreCheckLabelPrefix]; !ok {
					lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, id)) // pre-check
				}
			}
		}

		if _, ok := labels[v1alpha1.PodPostCheckedLabelPrefix]; ok { // post-checked
			if _, ok := labels[v1alpha1.PodCompleteLabelPrefix]; !ok {
				lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, id)) // complete, wait fo podopslifecycle controller adds readiness gate
			}
		}

		if _, ok := labels[v1alpha1.PodOperateLabelPrefix]; ok {
			operateCount++
		}
		if _, ok := labels[v1alpha1.PodOperatedLabelPrefix]; ok {
			operatedCount++
		}
		if _, ok := labels[v1alpha1.PodCompleteLabelPrefix]; ok { // complete
			completeCount++
		}
	}
	klog.Infof("pod: %s/%s, numOfIDs: %d, operatingCount: %d, operateCount: %d, operatedCount: %d, completeCount: %d", newPod.Namespace, newPod.Name, numOfIDs, operatingCount, operateCount, operatedCount, completeCount)

	for t, num := range undoTypeToNumsMap {
		if num == typeToNumsMap[t] { // reset the permission with type t if all operating with type t are canceled
			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, t))
		}
	}

	if operatingCount != 0 { // when operation is done, controller will remove operating label and operation type label
		return nil
	}

	if completeCount == numOfIDs { // all operations are completed
		satisfied, expectedFinalizer, err := lc.satisfyExpectedFinalizers(newPod) // whether all expected finalizers are satisfied
		if err != nil || !satisfied {
			klog.Infof("pod: %s/%s, expected finalizers: %v, err: %v", newPod.Namespace, newPod.Name, expectedFinalizer, err)
			return err
		}
		if !lc.isPodReady(newPod) {
			return nil
		}

		// all operations are done and all expected finalizers are satisfied, then remove all unuseful labels, and add service available label
		for id := range newIDToLabelsMap {
			for _, v := range []string{v1alpha1.PodOperateLabelPrefix,
				v1alpha1.PodOperatedLabelPrefix,
				v1alpha1.PodDoneOperationTypeLabelPrefix,
				v1alpha1.PodPostCheckLabelPrefix,
				v1alpha1.PodPostCheckedLabelPrefix,
				v1alpha1.PodCompleteLabelPrefix} {

				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}
		}
		lc.addLabelWithTime(newPod, v1alpha1.PodServiceAvailableLabel)

		return nil
	}

	if operateCount == numOfIDs { // all operations are going to be done
		oldIdToLabelsMap, _, err := podopslifecycle.PodIDAndTypesMap(oldPod)
		if err != nil {
			return err
		}

		for id, labels := range newIDToLabelsMap {
			for _, v := range []string{v1alpha1.PodPreCheckLabelPrefix, v1alpha1.PodPreCheckedLabelPrefix} {
				delete(newPod.Labels, fmt.Sprintf("%s/%s", v, id))
			}

			if _, ok := labels[v1alpha1.PodOperatedLabelPrefix]; !ok {
				lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, id)) // operated
				operatedCount++
			}

			t, ok := oldIdToLabelsMap[id][v1alpha1.PodOperationTypeLabelPrefix]
			if !ok {
				continue
			}
			delete(newPod.Labels, fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, t))

			newPod.Labels[fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, id)] = t // done-operation-type
		}
	}

	if operatedCount == numOfIDs { // all operations are done
		for id, labels := range newIDToLabelsMap {
			if _, ok := labels[v1alpha1.PodPostCheckLabelPrefix]; !ok {
				lc.addLabelWithTime(newPod, fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, id)) // post-check
			}
		}
	}

	return nil
}
