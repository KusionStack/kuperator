/*
Copyright 2024 The KusionStack Authors.

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

package synccontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontext"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
)

const (
	ReplaceNewPodIDContextDataKey    = "ReplaceNewPodID"
	ReplaceOriginPodIDContextDataKey = "ReplaceOriginPodID"
)

func (r *RealSyncControl) cleanReplacePodLabels(
	needCleanLabelPods []*corev1.Pod,
	podsNeedCleanLabels [][]string,
	ownedIDs map[int]*appsv1alpha1.ContextDetail) (bool, sets.String, error) {

	needUpdateContext := false
	needDeletePodsIDs := sets.String{}
	mapOriginToNewPodContext := mapReplaceOriginToNewPodContext(ownedIDs)
	mapNewToOriginPodContext := mapReplaceNewToOriginPodContext(ownedIDs)
	_, err := controllerutils.SlowStartBatch(len(needCleanLabelPods), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		pod := needCleanLabelPods[i]
		needCleanLabels := podsNeedCleanLabels[i]
		var deletePatch []map[string]string
		for _, labelKey := range needCleanLabels {
			patchOperation := map[string]string{
				"op":   "remove",
				"path": fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(labelKey, "/", "~1")),
			}
			deletePatch = append(deletePatch, patchOperation)
			// replace finished, (1) remove ReplaceNewPodID, ReplaceOriginPodID key from IDs, (2) try to delete origin Pod's ID
			if labelKey == appsv1alpha1.PodReplacePairOriginName {
				needUpdateContext = true
				newPodId, _ := collasetutils.GetPodInstanceID(pod)
				if originPodContext, exist := mapOriginToNewPodContext[newPodId]; exist && originPodContext != nil {
					originPodContext.Remove(ReplaceNewPodIDContextDataKey)
					needDeletePodsIDs.Insert(strconv.Itoa(originPodContext.ID))
				}
				ownedIDs[newPodId].Remove(ReplaceOriginPodIDContextDataKey)
			}
			// replace canceled, (1) remove ReplaceNewPodID, ReplaceOriginPodID key from IDs, (2) try to delete new Pod's ID
			_, replaceIndicate := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			if !replaceIndicate && labelKey == appsv1alpha1.PodReplacePairNewId {
				needUpdateContext = true
				originPodId, _ := collasetutils.GetPodInstanceID(pod)
				if newPodContext, exist := mapNewToOriginPodContext[originPodId]; exist && newPodContext != nil {
					newPodContext.Remove(ReplaceOriginPodIDContextDataKey)
					needDeletePodsIDs.Insert(strconv.Itoa(newPodContext.ID))
				}
				ownedIDs[originPodId].Remove(ReplaceNewPodIDContextDataKey)
			}
		}
		// patch to bytes
		patchBytes, err := json.Marshal(deletePatch)
		if err != nil {
			return err
		}
		if err = r.podControl.PatchPod(pod, client.RawPatch(types.JSONPatchType, patchBytes)); err != nil {
			return fmt.Errorf("failed to remove replace pair label %s/%s: %s", pod.Namespace, pod.Name, err)
		}
		return nil
	})

	return needUpdateContext, needDeletePodsIDs, err
}

func (r *RealSyncControl) replaceOriginPods(
	ctx context.Context,
	instance *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources,
	needReplaceOriginPods []*corev1.Pod,
	ownedIDs map[int]*appsv1alpha1.ContextDetail,
	currentIDs map[int]struct{}) (int, error) {

	availableContexts := extractAvailableContexts(len(needReplaceOriginPods), ownedIDs, currentIDs)
	mapNewToOriginPodContext := mapReplaceNewToOriginPodContext(ownedIDs)
	successCount, err := controllerutils.SlowStartBatch(len(needReplaceOriginPods), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		originPod := needReplaceOriginPods[i]
		originPodId, _ := collasetutils.GetPodInstanceID(originPod)
		ownerRef := metav1.NewControllerRef(instance, instance.GroupVersionKind())
		updatedPDs, err := resources.PDGetter.GetEffective(ctx, originPod)
		replaceRevision := getReplaceRevision(originPod, resources)
		if err != nil {
			return err
		}
		// create pod using update revision if replaced by update, otherwise using current revision
		newPod, err := collasetutils.NewPodFrom(instance, ownerRef, replaceRevision, func(in *corev1.Pod) error {
			return utilspoddecoration.PatchListOfDecorations(in, updatedPDs)
		})
		if err != nil {
			return err
		}
		// add instance id and replace pair label
		var instanceId string
		var newPodContext *appsv1alpha1.ContextDetail
		if contextDetail, exist := mapNewToOriginPodContext[originPodId]; exist && contextDetail != nil {
			newPodContext = contextDetail
			// reuse podContext ID if pair-relation exists
			instanceId = fmt.Sprintf("%d", newPodContext.ID)
			newPod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = instanceId
		} else {
			newPodContext = availableContexts[i]
			// add replace pair-relation to podContexts for originPod and newPod
			instanceId = fmt.Sprintf("%d", newPodContext.ID)
			newPod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = instanceId
			newPodId, _ := collasetutils.GetPodInstanceID(newPod)
			ownedIDs[originPodId].Put(ReplaceNewPodIDContextDataKey, strconv.Itoa(newPodId))
			ownedIDs[newPodId].Put(ReplaceOriginPodIDContextDataKey, strconv.Itoa(originPodId))
			ownedIDs[newPodId].Remove(podcontext.JustCreateContextDataKey)
		}
		newPod.Labels[appsv1alpha1.PodReplacePairOriginName] = originPod.GetName()
		newPodContext.Put(podcontext.RevisionContextDataKey, replaceRevision.Name)
		// create pvcs for new pod
		err = r.pvcControl.CreatePodPvcs(ctx, instance, newPod, resources.ExistingPvcs)
		if err != nil {
			return fmt.Errorf("fail to migrate PVCs from origin pod %s to replace pod %s: %s", originPod.Name, newPod.Name, err)
		}
		if newCreatedPod, err := r.podControl.CreatePod(newPod); err == nil {
			r.recorder.Eventf(originPod,
				corev1.EventTypeNormal,
				"CreatePairPod",
				"succeed to create replace pair Pod %s/%s with revision %s by replace",
				originPod.Namespace,
				originPod.Name,
				replaceRevision.Name)
			if err := collasetutils.ActiveExpectations.ExpectCreate(instance, expectations.Pod, newCreatedPod.Name); err != nil {
				return err
			}

			patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`, appsv1alpha1.PodReplacePairNewId, instanceId)))
			if err = r.podControl.PatchPod(originPod, patch); err != nil {
				return fmt.Errorf("fail to update origin pod %s/%s pair label %s when updating by replaceUpdate: %s", originPod.Namespace, originPod.Name, newCreatedPod.Name, err)
			}
		} else {
			r.recorder.Eventf(originPod,
				corev1.EventTypeNormal,
				"ReplacePod",
				"failed to create replace pair Pod %s/%s to from revision %s to revision %s by replace update",
				originPod.Namespace,
				originPod.Name,
				replaceRevision.Name)
			return err
		}
		return nil
	})

	return successCount, err
}

func dealReplacePods(pods []*corev1.Pod, instance *appsv1alpha1.CollaSet) (needReplacePods []*corev1.Pod, needCleanLabelPods []*corev1.Pod, podNeedCleanLabels [][]string, needDeletePods []*corev1.Pod, replaceIndicateCount int) {
	var podInstanceIdMap = make(map[string]*corev1.Pod)
	var podNameMap = make(map[string]*corev1.Pod)
	for _, pod := range pods {
		if instanceId, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]; exist {
			podInstanceIdMap[instanceId] = pod
		}
		podNameMap[pod.Name] = pod
	}

	// deal need replace pods
	for _, pod := range pods {
		// no replace indication label
		if _, exist := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; !exist {
			continue
		}
		replaceIndicateCount++

		// pod is replace new created pod, skip replace
		if originPodName, exist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
			if _, exist := podNameMap[originPodName]; exist {
				continue
			}
		}

		// pod already has a new created pod for replacement
		if newPairPodId, exist := pod.Labels[appsv1alpha1.PodReplacePairNewId]; exist {
			if _, exist := podInstanceIdMap[newPairPodId]; exist {
				continue
			}
		}

		needReplacePods = append(needReplacePods, pod)
	}
	isReplaceUpdate := instance.Spec.UpdateStrategy.PodUpdatePolicy == appsv1alpha1.CollaSetReplacePodUpdateStrategyType
	// deal pods need to delete when pod update strategy is not replace update

	for _, pod := range pods {
		_, inReplace := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
		_, replaceByUpdate := pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
		var needCleanLabels []string
		if inReplace && replaceByUpdate && !isReplaceUpdate {
			needCleanLabels = []string{appsv1alpha1.PodReplaceIndicationLabelKey, appsv1alpha1.PodReplaceByReplaceUpdateLabelKey}
		}

		// pod is replace new created pod, skip replace
		if originPodName, exist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
			// replace pair origin pod is not exist, clean label.
			if originPod, exist := podNameMap[originPodName]; !exist {
				needCleanLabels = append(needCleanLabels, appsv1alpha1.PodReplacePairOriginName)
			} else if originPod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] == "" {
				// replace canceled, delete replace new pod if origin pod is active
				if originPod.DeletionTimestamp == nil {
					needDeletePods = append(needDeletePods, pod)
				}
			} else if !replaceByUpdate {
				// not replace update, delete origin pod when new created pod is service available
				if _, serviceAvailable := pod.Labels[appsv1alpha1.PodServiceAvailableLabel]; serviceAvailable {
					needDeletePods = append(needDeletePods, originPod)
				}
			}
		}

		if newPairPodId, exist := pod.Labels[appsv1alpha1.PodReplacePairNewId]; exist {
			if newPod, exist := podInstanceIdMap[newPairPodId]; !exist {
				needCleanLabels = append(needCleanLabels, appsv1alpha1.PodReplacePairNewId)
			} else if replaceByUpdate && !isReplaceUpdate {
				needDeletePods = append(needDeletePods, newPod)
			}
		}

		if len(needCleanLabels) > 0 {
			needCleanLabelPods = append(needCleanLabelPods, pod)
			podNeedCleanLabels = append(podNeedCleanLabels, needCleanLabels)
		}
	}

	return
}

func getReplaceRevision(originPod *corev1.Pod, resources *collasetutils.RelatedResources) *appsv1.ControllerRevision {
	if _, exist := originPod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]; exist {
		return resources.UpdatedRevision
	}
	podCurrentRevisionName, exist := originPod.Labels[appsv1.ControllerRevisionHashLabelKey]
	if !exist {
		return resources.CurrentRevision
	}

	for _, revision := range resources.Revisions {
		if revision.Name == podCurrentRevisionName {
			return revision
		}
	}

	return resources.CurrentRevision
}

// classify the pair relationship for Pod replacement.
func classifyPodReplacingMapping(podWrappers []*collasetutils.PodWrapper) map[string]*collasetutils.PodWrapper {
	var podNameMap = make(map[string]*collasetutils.PodWrapper)
	var podIdMap = make(map[string]*collasetutils.PodWrapper)
	for _, podWrapper := range podWrappers {
		podNameMap[podWrapper.Name] = podWrapper
		instanceId := podWrapper.Labels[appsv1alpha1.PodInstanceIDLabelKey]
		podIdMap[instanceId] = podWrapper
	}

	var replacePodMapping = make(map[string]*collasetutils.PodWrapper)
	for _, podWrapper := range podWrappers {
		name := podWrapper.Name
		if podWrapper.DeletionTimestamp != nil {
			replacePodMapping[name] = nil
			continue
		}

		if replacePairNewIdStr, exist := podWrapper.Labels[appsv1alpha1.PodReplacePairNewId]; exist {
			if pairNewPod, exist := podIdMap[replacePairNewIdStr]; exist {
				replacePodMapping[name] = pairNewPod
				continue
			}
		} else if replaceOriginStr, exist := podWrapper.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
			if originPod, exist := podNameMap[replaceOriginStr]; exist {
				if originPod.Labels[appsv1alpha1.PodReplacePairNewId] == podWrapper.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
					continue
				}
			}
		}

		replacePodMapping[name] = nil
	}
	return replacePodMapping
}

func mapReplaceNewToOriginPodContext(ownedIDs map[int]*appsv1alpha1.ContextDetail) map[int]*appsv1alpha1.ContextDetail {
	mapNewToOriginPodContext := make(map[int]*appsv1alpha1.ContextDetail)
	for id, contextDetail := range ownedIDs {
		if val, exist := contextDetail.Data[ReplaceNewPodIDContextDataKey]; exist {
			newPodId, _ := strconv.ParseInt(val, 10, 32)
			newPodContextDetail, exist := ownedIDs[int(newPodId)]
			if exist && newPodContextDetail.Data[ReplaceOriginPodIDContextDataKey] == strconv.Itoa(id) {
				mapNewToOriginPodContext[id] = newPodContextDetail
			} else {
				mapNewToOriginPodContext[id] = nil
			}
		}
	}
	return mapNewToOriginPodContext
}

func mapReplaceOriginToNewPodContext(ownedIDs map[int]*appsv1alpha1.ContextDetail) map[int]*appsv1alpha1.ContextDetail {
	mapOriginToNewPodContext := make(map[int]*appsv1alpha1.ContextDetail)
	for id, contextDetail := range ownedIDs {
		if val, exist := contextDetail.Data[ReplaceOriginPodIDContextDataKey]; exist {
			originPodId, _ := strconv.ParseInt(val, 10, 32)
			originPodContextDetail, exist := ownedIDs[int(originPodId)]
			if exist && originPodContextDetail.Data[ReplaceNewPodIDContextDataKey] == strconv.Itoa(id) {
				mapOriginToNewPodContext[id] = originPodContextDetail
			} else {
				mapOriginToNewPodContext[id] = nil
			}
		}
	}
	return mapOriginToNewPodContext
}
