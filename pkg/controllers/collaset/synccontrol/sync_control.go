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

package synccontrol

import (
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/collaset/podcontext"
	"kusionstack.io/kafed/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/kafed/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/kafed/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/kafed/pkg/controllers/utils"
	"kusionstack.io/kafed/pkg/controllers/utils/expectations"
	"kusionstack.io/kafed/pkg/controllers/utils/podopslifecycle"
)

const (
	ScaleInContextDataKey = "ScaleIn"
)

type Interface interface {
	SyncPods(instance *appsv1alpha1.CollaSet, updatedRevision *appsv1.ControllerRevision, newStatus *appsv1alpha1.CollaSetStatus) (bool, []*collasetutils.PodWrapper, map[int]*appsv1alpha1.ContextDetail, error)
	Scale(instance *appsv1alpha1.CollaSet, filteredPods []*collasetutils.PodWrapper, revisions []*appsv1.ControllerRevision, updatedRevision *appsv1.ControllerRevision, ownedIDs map[int]*appsv1alpha1.ContextDetail, newStatus *appsv1alpha1.CollaSetStatus) (bool, error)
	Update(instance *appsv1alpha1.CollaSet, filteredPods []*collasetutils.PodWrapper, revisions []*appsv1.ControllerRevision, updatedRevision *appsv1.ControllerRevision, ownedIDs map[int]*appsv1alpha1.ContextDetail, newStatus *appsv1alpha1.CollaSetStatus) (bool, error)
}

func NewRealSyncControl(client client.Client, podControl podcontrol.Interface, recorder record.EventRecorder) *RealSyncControl {
	return &RealSyncControl{
		client:     client,
		podControl: podControl,
		recorder:   recorder,
	}
}

type RealSyncControl struct {
	client     client.Client
	podControl podcontrol.Interface
	recorder   record.EventRecorder
}

// SyncPods is used to reclaim Pod instance ID
func (sc *RealSyncControl) SyncPods(instance *appsv1alpha1.CollaSet, updatedRevision *appsv1.ControllerRevision, _ *appsv1alpha1.CollaSetStatus) (bool, []*collasetutils.PodWrapper, map[int]*appsv1alpha1.ContextDetail, error) {
	filteredPods, err := sc.podControl.GetFilteredPods(instance.Spec.Selector, instance)
	if err != nil {
		return false, nil, nil, fmt.Errorf("fail to get filtered Pods: %s", err)
	}

	// get owned IDs
	var ownedIDs map[int]*appsv1alpha1.ContextDetail
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		ownedIDs, err = podcontext.AllocateID(sc.client, instance, updatedRevision.Name, int(replicasRealValue(instance.Spec.Replicas)))
		return err
	}); err != nil {
		return false, nil, ownedIDs, fmt.Errorf("fail to allocate %d IDs using context when sync Pods: %s", instance.Spec.Replicas, err)
	}

	// wrap Pod with more information
	var podWrappers []*collasetutils.PodWrapper

	// stateless case
	currentIDs := sets.Int{}
	idToReclaim := sets.Int{}
	for i := range filteredPods {
		pod := filteredPods[i]
		id, _ := collasetutils.GetPodInstanceID(pod)
		if pod.DeletionTimestamp != nil {
			// 1. Reclaim ID from Pod which is scaling in and terminating.
			if contextDetail, exist := ownedIDs[id]; exist && contextDetail.Contains(ScaleInContextDataKey, "true") {
				idToReclaim.Insert(id)
			}

			// 2. filter out Pods which are terminating
			continue
		}

		podWrappers = append(podWrappers, &collasetutils.PodWrapper{
			Pod:           pod,
			ID:            id,
			ContextDetail: ownedIDs[id],
		})

		if id >= 0 {
			currentIDs.Insert(id)
		}
	}

	// 3. Reclaim Pod ID which Pod & PVC are all non-existing
	for id, contextDetail := range ownedIDs {
		if contextDetail.Contains(ScaleInContextDataKey, "true") && !currentIDs.Has(id) {
			idToReclaim.Insert(id)
		}
	}

	needUpdateContext := false
	for _, id := range idToReclaim.List() {
		needUpdateContext = true
		delete(ownedIDs, id)
	}

	// TODO stateful case
	// 1. only reclaim non-existing Pods' ID. Do not reclaim terminating Pods' ID until these Pods and PVC have been deleted from ETCD
	// 2. do not filter out these terminating Pods

	if needUpdateContext {
		klog.V(1).Infof("try to update ResourceContext for CollaSet %s/%s when sync", instance.Namespace, instance.Name)
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return podcontext.UpdateToPodContext(sc.client, instance, ownedIDs)
		}); err != nil {
			return false, nil, ownedIDs, fmt.Errorf("fail to update ResourceContext when reclaiming IDs: %s", err)
		}
	}

	return false, podWrappers, ownedIDs, nil
}

func (sc *RealSyncControl) Scale(set *appsv1alpha1.CollaSet, podWrappers []*collasetutils.PodWrapper, revisions []*appsv1.ControllerRevision, updatedRevision *appsv1.ControllerRevision, ownedIDs map[int]*appsv1alpha1.ContextDetail, newStatus *appsv1alpha1.CollaSetStatus) (scaled bool, err error) {
	diff := int(replicasRealValue(set.Spec.Replicas)) - len(podWrappers)
	scaling := false

	if diff > 0 {
		// collect instance ID in used from owned Pods
		podInstanceIDSet := collasetutils.CollectPodInstanceID(podWrappers)
		// find IDs and their contexts which have not been used by owned Pods
		availableContext := extractAvailableContexts(diff, ownedIDs, podInstanceIDSet)

		succCount, err := controllerutils.SlowStartBatch(diff, controllerutils.SlowStartInitialBatchSize, false, func(idx int, _ error) error {
			availableIDContext := availableContext[idx]
			// use revision recorded in Context
			revision := updatedRevision
			if revisionName, exist := availableIDContext.Data[podcontext.RevisionContextDataKey]; exist && revisionName != "" {
				for i := range revisions {
					if revisions[i].Name == revisionName {
						revision = revisions[i]
						break
					}
				}
			}

			// scale out new Pods with updatedRevision
			// TODO use cache
			pod, err := collasetutils.NewPodFrom(set, metav1.NewControllerRef(set, appsv1alpha1.GroupVersion.WithKind("CollaSet")), revision)
			if err != nil {
				return fmt.Errorf("fail to new Pod from revision %s: %s", revision.Name, err)
			}
			newPod := pod.DeepCopy()
			// allocate new Pod a instance ID
			newPod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = fmt.Sprintf("%d", availableIDContext.ID)

			klog.V(1).Info("try to create Pod with revision %s from CollaSet %s/%s", revision.Name, set.Namespace, set.Name)
			if pod, err = sc.podControl.CreatePod(newPod); err != nil {
				return err
			}

			// add an expectation for this pod creation, before next reconciling
			return collasetutils.ActiveExpectations.ExpectCreate(set, expectations.Pod, pod.Name)
		})

		sc.recorder.Eventf(set, corev1.EventTypeNormal, "ScaleOut", "scale out %d Pod(s)", succCount)
		if err != nil {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, err, "ScaleOutFailed", err.Error())
			return succCount > 0, err
		}
		collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, nil, "ScaleOut", "")

		return succCount > 0, err
	} else if diff < 0 {
		// chose the pods to scale in
		podsToScaleIn := getPodsToDelete(podWrappers, diff*-1)
		// filter out Pods need to trigger PodOpsLifecycle
		podCh := make(chan *collasetutils.PodWrapper, len(podsToScaleIn))
		for i := range podsToScaleIn {
			if podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, podsToScaleIn[i].Pod) {
				continue
			}
			podCh <- podsToScaleIn[i]
		}

		// trigger Pods to enter PodOpsLifecycle
		succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(_ int, err error) error {
			pod := <-podCh

			// trigger PodOpsLifecycle with scaleIn OperationType
			klog.V(1).Infof("try to begin PodOpsLifecycle for scaling in Pod %s/%s in CollaSet %s/%s", pod.Namespace, pod.Name, set.Namespace, set.Name)
			if updated, err := podopslifecycle.Begin(sc.client, collasetutils.ScaleInOpsLifecycleAdapter, pod.Pod); err != nil {
				return fmt.Errorf("fail to begin PodOpsLifecycle for Scaling in Pod %s/%s: %s", pod.Namespace, pod.Name, err)
			} else if updated {
				sc.recorder.Eventf(pod.Pod, corev1.EventTypeNormal, "BeginScaleInLifecycle", "succeed to begin PodOpsLifecycle for scaling in")
				// add an expectation for this pod creation, before next reconciling
				if err := collasetutils.ActiveExpectations.ExpectUpdate(set, expectations.Pod, pod.Name, pod.ResourceVersion); err != nil {
					return err
				}
			}

			return nil
		})
		scaling = succCount != 0

		if err != nil {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, err, "ScaleInFailed", err.Error())
			return scaling, err
		} else {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, nil, "ScaleIn", "")
		}

		needUpdateContext := false
		for i, podWrapper := range podsToScaleIn {
			if !podopslifecycle.AllowOps(collasetutils.ScaleInOpsLifecycleAdapter, podWrapper.Pod) && podWrapper.DeletionTimestamp == nil {
				sc.recorder.Eventf(podWrapper.Pod, corev1.EventTypeNormal, "PodScaleInLifecycle", "Pod is not allowed to scale in")
				continue
			}

			// if Pod is allowed to operate or Pod has already been deleted, promte to delete Pod
			if podWrapper.ID >= 0 && !ownedIDs[podWrapper.ID].Contains(ScaleInContextDataKey, "true") {
				needUpdateContext = true
				ownedIDs[podWrapper.ID].Put(ScaleInContextDataKey, "true")
			}

			if podWrapper.DeletionTimestamp != nil {
				continue
			}

			podCh <- podsToScaleIn[i]
		}

		// mark these Pods to scalingIn
		if needUpdateContext {
			klog.V(1).Infof("try to update ResourceContext for CollaSet %s/%s when scaling in Pod", set.Namespace, set.Name)
			err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				return podcontext.UpdateToPodContext(sc.client, set, ownedIDs)
			})

			if err != nil {
				collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, err, "ScaleInFailed", fmt.Sprintf("failed to update Context for scaling in: %s", err))
				return scaling, err
			} else {
				collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, nil, "ScaleIn", "")
			}
		}

		// do delete Pod resource
		succCount, err = controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
			pod := <-podCh
			klog.V(1).Infof("try to scale in Pod %s/%s", pod.Namespace, pod.Name)
			if err := sc.podControl.DeletePod(pod.Pod); err != nil {
				return fmt.Errorf("fail to delete Pod %s/%s when scaling in: %s", pod.Namespace, pod.Name, err)
			}

			sc.recorder.Eventf(set, corev1.EventTypeNormal, "PodDeleted", "succeed to scale in Pod %s/%s", pod.Namespace, pod.Name)
			if err := collasetutils.ActiveExpectations.ExpectDelete(set, expectations.Pod, pod.Name); err != nil {
				return err
			}

			// TODO also need to delete PVC from PVC template here

			return nil
		})
		scaling := scaling || succCount > 0

		if succCount > 0 {
			sc.recorder.Eventf(set, corev1.EventTypeNormal, "ScaleIn", "scale in %d Pod(s)", succCount)
		}
		if err != nil {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, err, "ScaleInFailed", fmt.Sprintf("fail to delete Pod for scaling in: %s", err))
			return scaling, err
		} else {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, nil, "ScaleIn", "")
		}

		return scaling, err
	}

	// reset ContextDetail.ScalingIn, if there are Pods had its PodOpsLifecycle reverted
	needUpdatePodContext := false
	for _, podWrapper := range podWrappers {
		if !podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, podWrapper) && ownedIDs[podWrapper.ID].Contains(ScaleInContextDataKey, "true") {
			needUpdatePodContext = true
			ownedIDs[podWrapper.ID].Remove(ScaleInContextDataKey)
		}
	}

	if needUpdatePodContext {
		klog.V(1).Infof("try to update ResourceContext for CollaSet %s/%s after scaling", set.Namespace, set.Name)
		if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return podcontext.UpdateToPodContext(sc.client, set, ownedIDs)
		}); err != nil {
			return scaling, fmt.Errorf("fail to reset ResourceContext: %s", err)
		}
	}

	return scaling, nil
}

func extractAvailableContexts(diff int, ownedIDs map[int]*appsv1alpha1.ContextDetail, podInstanceIDSet map[int]struct{}) []*appsv1alpha1.ContextDetail {
	availableContexts := make([]*appsv1alpha1.ContextDetail, diff)

	idx := 0
	for id := range ownedIDs {
		if _, inUsed := podInstanceIDSet[id]; inUsed {
			continue
		}

		availableContexts[idx] = ownedIDs[id]
		idx++
	}

	return availableContexts
}

func (sc *RealSyncControl) Update(set *appsv1alpha1.CollaSet, podWrapers []*collasetutils.PodWrapper, revisions []*appsv1.ControllerRevision, updatedRevision *appsv1.ControllerRevision, ownedIDs map[int]*appsv1alpha1.ContextDetail, newStatus *appsv1alpha1.CollaSetStatus) (bool, error) {
	// 1. scan and analysis pods update info
	podUpdateInfos := attachPodUpdateInfo(podWrapers, revisions, updatedRevision)

	// 2. decide Pod update candidates
	podToUpdate := decidePodToUpdate(set, podUpdateInfos)

	// 3. prepare Pods to begin PodOpsLifecycle
	podCh := make(chan *PodUpdateInfo, len(podToUpdate))
	for _, podInfo := range podToUpdate {
		if podInfo.IsUpdatedRevision {
			continue
		}

		if podopslifecycle.IsDuringOps(utils.UpdateOpsLifecycleAdapter, podInfo) {
			continue
		}

		podCh <- podInfo
	}

	// 4. begin podOpsLifecycle parallel
	updater := newPodUpdater(set)
	updating := false
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(_ int, err error) error {
		podInfo := <-podCh

		klog.V(1).Infof("try to begin PodOpsLifecycle for updating Pod %s/%s of CollaSet %s/%s", podInfo.Namespace, podInfo.Name, set.Namespace, set.Name)
		if updated, err := podopslifecycle.Begin(sc.client, utils.UpdateOpsLifecycleAdapter, podInfo.Pod); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
		} else if updated {
			// add an expectation for this pod update, before next reconciling
			if err := collasetutils.ActiveExpectations.ExpectUpdate(set, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
				return err
			}
		}

		return nil
	})

	updating = updating || succCount > 0
	if err != nil {
		collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetUpdate, err, "UpdateFailed", err.Error())
		return updating, err
	} else {
		collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}

	needUpdateContext := false
	for i := range podToUpdate {
		podInfo := podToUpdate[i]
		if !podopslifecycle.AllowOps(utils.UpdateOpsLifecycleAdapter, podInfo) {
			sc.recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "Pod is not allowed to update")
			continue
		}

		if !ownedIDs[podInfo.ID].Contains(podcontext.RevisionContextDataKey, updatedRevision.Name) {
			needUpdateContext = true
			ownedIDs[podInfo.ID].Put(podcontext.RevisionContextDataKey, updatedRevision.Name)
		}

		if podInfo.IsUpdatedRevision {
			continue
		}

		// if Pod has not been updated, update it.
		podCh <- podToUpdate[i]
	}

	// 5. mark Pod to use updated revision before updating it.
	if needUpdateContext {
		klog.V(1).Infof("try to update ResourceContext for CollaSet %s/%s", set.Namespace, set.Name)
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			return podcontext.UpdateToPodContext(sc.client, set, ownedIDs)
		})

		if err != nil {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, err, "UpdateFailed", fmt.Sprintf("fail to update Context for updating: %s", err))
			return updating, err
		} else {
			collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetScale, nil, "UpdateFailed", "")
		}
	}

	// 6. update Pod
	succCount, err = controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(_ int, _ error) error {
		podInfo := <-podCh

		// analyse Pod to get update information
		inPlaceSupport, onlyMetadataChanged, updatedPod, err := updater.AnalyseAndGetUpdatedPod(set, updatedRevision, podInfo)
		if err != nil {
			return fmt.Errorf("fail to analyse pod %s/%s in-place update support: %s", podInfo.Namespace, podInfo.Name, err)
		}

		klog.V(1).Infof("Pod %s/%s update operation from revision %s to revision %s, is [%t] in-place update supported and [%t] only has metadata changed.",
			podInfo.Namespace, podInfo.Name, podInfo.CurrentRevision.Name, updatedRevision.Namespace, inPlaceSupport, onlyMetadataChanged)
		if onlyMetadataChanged || inPlaceSupport {
			// 6.1 if pod template changes only include metadata or support in-place update, just apply these changes to pod directly
			if err = sc.podControl.UpdatePod(updatedPod); err != nil {
				return fmt.Errorf("fail to update Pod %s/%s when updating by in-place: %s", podInfo.Namespace, podInfo.Name, err)
			} else {
				podInfo.Pod = updatedPod
				sc.recorder.Eventf(podInfo.Pod, corev1.EventTypeNormal, "UpdatePod", "succeed to update Pod %s/%s to from revision %s to revision %s by in-place", podInfo.Namespace, podInfo.Name, podInfo.CurrentRevision.Name, updatedRevision.Name)
				if err := collasetutils.ActiveExpectations.ExpectUpdate(set, expectations.Pod, podInfo.Name, updatedPod.ResourceVersion); err != nil {
					return err
				}
			}
		} else {
			// 6.2 if pod has changes not in-place supported, recreate it
			if err = sc.podControl.DeletePod(podInfo.Pod); err != nil {
				return fmt.Errorf("fail to delete Pod %s/%s when updating by recreate: %s", podInfo.Namespace, podInfo.Name, err)
			} else {
				sc.recorder.Eventf(podInfo.Pod, corev1.EventTypeNormal, "UpdatePod", "succeed to update Pod %s/%s to from revision %s to revision %s by recreate", podInfo.Namespace, podInfo.Name, podInfo.CurrentRevision.Name, updatedRevision.Name)
				if err := collasetutils.ActiveExpectations.ExpectDelete(set, expectations.Pod, podInfo.Name); err != nil {
					return err
				}
			}
		}

		return nil
	})

	updating = updating || succCount > 0
	if err != nil {
		collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetUpdate, err, "UpdateFailed", err.Error())
		return updating, err
	} else {
		collasetutils.AddOrUpdateCondition(newStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}

	// try to finish all Pods'PodOpsLifecycle if its update is finished.
	succCount, err = controllerutils.SlowStartBatch(len(podUpdateInfos), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		podInfo := podUpdateInfos[i]

		if !podInfo.isDuringOps {
			return nil
		}

		// check Pod is during updating, and it is finished or not
		finished, msg, err := updater.GetPodUpdateFinishStatus(podInfo)
		if err != nil {
			return fmt.Errorf("fail to get pod %s/%s update finished: %s", podInfo.Namespace, podInfo.Name, err)
		}

		if finished {
			klog.V(1).Infof("try to finish update PodOpsLifecycle for Pod %s/%s", podInfo.Namespace, podInfo.Name)
			if updated, err := podopslifecycle.Finish(sc.client, utils.UpdateOpsLifecycleAdapter, podInfo.Pod); err != nil {
				return fmt.Errorf("fail to finish PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
			} else if updated {
				// add an expectation for this pod update, before next reconciling
				if err := collasetutils.ActiveExpectations.ExpectUpdate(set, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
					return err
				}
				sc.recorder.Eventf(podInfo.Pod, corev1.EventTypeNormal, "UpdateReady", "pod %s/%s update finished", podInfo.Namespace, podInfo.Name)
			}
		} else {
			sc.recorder.Eventf(podInfo.Pod, corev1.EventTypeNormal, "WaitingUpdateReady", "waiting for pod %s/%s to update finished: %s", podInfo.Namespace, podInfo.Name, msg)
		}

		return nil
	})

	return updating || succCount > 0, err
}

func replicasRealValue(replcias *int32) int32 {
	if replcias == nil {
		return 0
	}

	return *replcias
}
