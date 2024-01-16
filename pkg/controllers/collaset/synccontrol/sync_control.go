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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontext"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	commonutils "kusionstack.io/operating/pkg/utils"
)

const (
	ScaleInContextDataKey = "ScaleIn"
)

type Interface interface {
	SyncPods(
		ctx context.Context,
		instance *appsv1alpha1.CollaSet,
		resources *collasetutils.RelatedResources,
	) (bool, []*collasetutils.PodWrapper, map[int]*appsv1alpha1.ContextDetail, error)

	Scale(
		ctx context.Context,
		instance *appsv1alpha1.CollaSet,
		resources *collasetutils.RelatedResources,
		filteredPods []*collasetutils.PodWrapper,
		ownedIDs map[int]*appsv1alpha1.ContextDetail,
	) (bool, *time.Duration, error)

	Update(
		ctx context.Context,
		instance *appsv1alpha1.CollaSet,
		resources *collasetutils.RelatedResources,
		filteredPods []*collasetutils.PodWrapper,
		ownedIDs map[int]*appsv1alpha1.ContextDetail,
	) (bool, *time.Duration, error)
}

func NewRealSyncControl(client client.Client, logger logr.Logger, podControl podcontrol.Interface, recorder record.EventRecorder) Interface {
	return &RealSyncControl{
		client:     client,
		logger:     logger,
		podControl: podControl,
		recorder:   recorder,
	}
}

var _ Interface = &RealSyncControl{}

type RealSyncControl struct {
	client     client.Client
	logger     logr.Logger
	podControl podcontrol.Interface
	recorder   record.EventRecorder
}

// SyncPods is used to reclaim Pod instance ID
func (r *RealSyncControl) SyncPods(
	ctx context.Context,
	instance *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources,
) (
	bool, []*collasetutils.PodWrapper, map[int]*appsv1alpha1.ContextDetail, error) {

	logger := r.logger.WithValues("collaset", commonutils.ObjectKeyString(instance))
	filteredPods, err := r.podControl.GetFilteredPods(instance.Spec.Selector, instance)
	if err != nil {
		return false, nil, nil, fmt.Errorf("fail to get filtered Pods: %s", err)
	}

	// get owned IDs
	var ownedIDs map[int]*appsv1alpha1.ContextDetail
	if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ownedIDs, err = podcontext.AllocateID(r.client, instance, resources.UpdatedRevision.Name, int(realValue(instance.Spec.Replicas)))
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
		logger.V(1).Info("try to update ResourceContext for CollaSet when sync")
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(r.client, instance, ownedIDs)
		}); err != nil {
			return false, nil, ownedIDs, fmt.Errorf("fail to update ResourceContext when reclaiming IDs: %s", err)
		}
	}

	return false, podWrappers, ownedIDs, nil
}

func (r *RealSyncControl) Scale(
	ctx context.Context,
	cls *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources,
	podWrappers []*collasetutils.PodWrapper,
	ownedIDs map[int]*appsv1alpha1.ContextDetail,
) (bool, *time.Duration, error) {

	logger := r.logger.WithValues("collaset", commonutils.ObjectKeyString(cls))
	var recordedRequeueAfter *time.Duration
	replacePodMap := classifyPodReplacingMapping(podWrappers)

	diff := int(realValue(cls.Spec.Replicas)) - len(replacePodMap)
	scaling := false

	if diff > 0 {
		// collect instance ID in used from owned Pods
		podInstanceIDSet := collasetutils.CollectPodInstanceID(podWrappers)
		// find IDs and their contexts which have not been used by owned Pods
		availableContext := extractAvailableContexts(diff, ownedIDs, podInstanceIDSet)

		succCount, err := controllerutils.SlowStartBatch(diff, controllerutils.SlowStartInitialBatchSize, false, func(idx int, _ error) error {
			availableIDContext := availableContext[idx]
			// use revision recorded in Context
			revision := resources.UpdatedRevision
			if revisionName, exist := availableIDContext.Data[podcontext.RevisionContextDataKey]; exist && revisionName != "" {
				for i := range resources.Revisions {
					if resources.Revisions[i].Name == revisionName {
						revision = resources.Revisions[i]
						break
					}
				}
			}

			// scale out new Pods with updatedRevision
			// TODO use cache
			pod, err := collasetutils.NewPodFrom(
				cls,
				metav1.NewControllerRef(cls, appsv1alpha1.GroupVersion.WithKind("CollaSet")),
				revision,
				func(in *corev1.Pod) (localErr error) {
					in.Labels[appsv1alpha1.PodInstanceIDLabelKey] = fmt.Sprintf("%d", availableIDContext.ID)
					revisionsInfo, ok := availableIDContext.Get(podcontext.PodDecorationRevisionKey)
					var pds map[string]*appsv1alpha1.PodDecoration
					if !ok {
						// get default PodDecorations if no revision in context
						pds, localErr = resources.PDGetter.GetLatestDecorationsByTargetLabel(ctx, in.Labels)
						if localErr != nil {
							return localErr
						}
					} else {
						// upgrade by recreate pod case
						infos, marshallErr := utilspoddecoration.UnmarshallFromString(revisionsInfo)
						if marshallErr != nil {
							return marshallErr
						}
						var revisions []string
						for _, info := range infos {
							revisions = append(revisions, info.Revision)
						}
						pds, localErr = resources.PDGetter.GetDecorationByRevisions(ctx, revisions...)
						if localErr != nil {
							return localErr
						}
					}
					logger.Info("get pod effective decorations before create it", "EffectivePodDecorations", utilspoddecoration.BuildInfo(pds))
					return utilspoddecoration.PatchListOfDecorations(in, pds)
				},
			)
			if err != nil {
				return fmt.Errorf("fail to new Pod from revision %s: %s", revision.Name, err)
			}
			newPod := pod.DeepCopy()
			logger.V(1).Info("try to create Pod with revision of collaSet", "revision", revision.Name)
			if pod, err = r.podControl.CreatePod(newPod); err != nil {
				return err
			}

			// add an expectation for this pod creation, before next reconciling
			return collasetutils.ActiveExpectations.ExpectCreate(cls, expectations.Pod, pod.Name)
		})

		r.recorder.Eventf(cls, corev1.EventTypeNormal, "ScaleOut", "scale out %d Pod(s)", succCount)
		if err != nil {
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, err, "ScaleOutFailed", err.Error())
			return succCount > 0, recordedRequeueAfter, err
		}
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, nil, "ScaleOut", "")

		return succCount > 0, recordedRequeueAfter, err
	} else if diff < 0 {
		// chose the pods to scale in
		podsToScaleIn := getPodsToDelete(podWrappers, replacePodMap, diff*-1)
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
			logger.V(1).Info("try to begin PodOpsLifecycle for scaling in Pod in CollaSet", "pod", commonutils.ObjectKeyString(pod))
			if updated, err := podopslifecycle.Begin(r.client, collasetutils.ScaleInOpsLifecycleAdapter, pod.Pod); err != nil {
				return fmt.Errorf("fail to begin PodOpsLifecycle for Scaling in Pod %s/%s: %s", pod.Namespace, pod.Name, err)
			} else if updated {
				r.recorder.Eventf(pod.Pod, corev1.EventTypeNormal, "BeginScaleInLifecycle", "succeed to begin PodOpsLifecycle for scaling in")
				// add an expectation for this pod creation, before next reconciling
				if err := collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, pod.Name, pod.ResourceVersion); err != nil {
					return err
				}
			}

			return nil
		})
		scaling = succCount != 0

		if err != nil {
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, err, "ScaleInFailed", err.Error())
			return scaling, recordedRequeueAfter, err
		} else {
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, nil, "ScaleIn", "")
		}

		needUpdateContext := false
		for i, podWrapper := range podsToScaleIn {
			requeueAfter, allowed := podopslifecycle.AllowOps(collasetutils.ScaleInOpsLifecycleAdapter, realValue(cls.Spec.ScaleStrategy.OperationDelaySeconds), podWrapper.Pod)
			if !allowed && podWrapper.DeletionTimestamp == nil {
				r.recorder.Eventf(podWrapper.Pod, corev1.EventTypeNormal, "PodScaleInLifecycle", "Pod is not allowed to scale in")
				continue
			}

			if requeueAfter != nil {
				r.recorder.Eventf(podWrapper.Pod, corev1.EventTypeNormal, "PodScaleInLifecycle", "delay Pod scale in for %d seconds", requeueAfter.Seconds())
				if recordedRequeueAfter == nil || *requeueAfter < *recordedRequeueAfter {
					recordedRequeueAfter = requeueAfter
				}

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
			logger.V(1).Info("try to update ResourceContext for CollaSet when scaling in Pod")
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				return podcontext.UpdateToPodContext(r.client, cls, ownedIDs)
			})

			if err != nil {
				collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, err, "ScaleInFailed", fmt.Sprintf("failed to update Context for scaling in: %s", err))
				return scaling, recordedRequeueAfter, err
			} else {
				collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, nil, "ScaleIn", "")
			}
		}

		// do delete Pod resource
		succCount, err = controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
			pod := <-podCh
			logger.V(1).Info("try to scale in Pod", "pod", commonutils.ObjectKeyString(pod))
			if err := r.podControl.DeletePod(pod.Pod); err != nil {
				return fmt.Errorf("fail to delete Pod %s/%s when scaling in: %s", pod.Namespace, pod.Name, err)
			}

			r.recorder.Eventf(cls, corev1.EventTypeNormal, "PodDeleted", "succeed to scale in Pod %s/%s", pod.Namespace, pod.Name)
			if err := collasetutils.ActiveExpectations.ExpectDelete(cls, expectations.Pod, pod.Name); err != nil {
				return err
			}

			// TODO also need to delete PVC from PVC template here

			return nil
		})
		scaling := scaling || succCount > 0

		if succCount > 0 {
			r.recorder.Eventf(cls, corev1.EventTypeNormal, "ScaleIn", "scale in %d Pod(s)", succCount)
		}
		if err != nil {
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, err, "ScaleInFailed", fmt.Sprintf("fail to delete Pod for scaling in: %s", err))
			return scaling, recordedRequeueAfter, err
		} else {
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, nil, "ScaleIn", "")
		}

		return scaling, recordedRequeueAfter, err
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
		logger.V(1).Info("try to update ResourceContext for CollaSet after scaling")
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(r.client, cls, ownedIDs)
		}); err != nil {
			return scaling, recordedRequeueAfter, fmt.Errorf("fail to reset ResourceContext: %s", err)
		}
	}

	return scaling, recordedRequeueAfter, nil
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

		if podWrapper.Labels != nil && podWrapper.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] != "" {
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

func (r *RealSyncControl) Update(
	ctx context.Context,
	cls *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources,
	podWrappers []*collasetutils.PodWrapper,
	ownedIDs map[int]*appsv1alpha1.ContextDetail,
) (bool, *time.Duration, error) {

	logger := r.logger.WithValues("collaset", commonutils.ObjectKeyString(cls))
	var recordedRequeueAfter *time.Duration
	// 1. scan and analysis pods update info
	podUpdateInfos, err := attachPodUpdateInfo(ctx, podWrappers, resources)
	if err != nil {
		return false, nil, fmt.Errorf("fail to attach pod update info, %v", err)
	}
	// 2. decide Pod update candidates
	podToUpdate := decidePodToUpdate(cls, podUpdateInfos)

	// 3. prepare Pods to begin PodOpsLifecycle
	podCh := make(chan *PodUpdateInfo, len(podToUpdate))
	for i, podInfo := range podToUpdate {
		// The pod is in a "replacing" state, always requires further update processing.
		if podInfo.isInReplacing {
			podCh <- podToUpdate[i]
			continue
		}

		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged {
			continue
		}

		if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, podInfo) {
			continue
		}
		podCh <- podToUpdate[i]
	}

	// 4. begin podOpsLifecycle parallel
	updating := false
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(_ int, err error) error {
		podInfo := <-podCh

		logger.V(1).Info("try to begin PodOpsLifecycle for updating Pod of CollaSet", "pod", commonutils.ObjectKeyString(podInfo.Pod))
		if updated, err := podopslifecycle.Begin(r.client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
		} else if updated {
			// add an expectation for this pod update, before next reconciling
			if err := collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
				return err
			}
		}

		return nil
	})

	updating = updating || succCount > 0
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, err, "UpdateFailed", err.Error())
		return updating, nil, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}

	needUpdateContext := false
	for i := range podToUpdate {
		podInfo := podToUpdate[i]
		requeueAfter, allowed := podopslifecycle.AllowOps(collasetutils.UpdateOpsLifecycleAdapter, realValue(cls.Spec.UpdateStrategy.OperationDelaySeconds), podInfo.Pod)
		if !allowed {
			r.recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "Pod %s is not allowed to update", commonutils.ObjectKeyString(podInfo.Pod))
			continue
		}
		if requeueAfter != nil {
			r.recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "delay Pod update for %d seconds", requeueAfter.Seconds())
			if recordedRequeueAfter == nil || *requeueAfter < *recordedRequeueAfter {
				recordedRequeueAfter = requeueAfter
			}
			continue
		}

		if !ownedIDs[podInfo.ID].Contains(podcontext.RevisionContextDataKey, resources.UpdatedRevision.Name) {
			needUpdateContext = true
			ownedIDs[podInfo.ID].Put(podcontext.RevisionContextDataKey, resources.UpdatedRevision.Name)
		}
		if podInfo.PodDecorationChanged {
			decorationStr := utilspoddecoration.GetDecorationInfoString(podInfo.UpdatedPodDecorations)
			if val, ok := ownedIDs[podInfo.ID].Get(podcontext.PodDecorationRevisionKey); !ok || val != decorationStr {
				needUpdateContext = true
				ownedIDs[podInfo.ID].Put(podcontext.PodDecorationRevisionKey, decorationStr)
			}
		}
		if podInfo.isInReplacing {
			podCh <- podToUpdate[i]
			continue
		}

		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged {
			continue
		}
		// if Pod has not been updated, update it.
		podCh <- podToUpdate[i]
	}

	// 5. mark Pod to use updated revision before updating it.
	if needUpdateContext {
		logger.V(1).Info("try to update ResourceContext for CollaSet")
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(r.client, cls, ownedIDs)
		})

		if err != nil {
			collasetutils.AddOrUpdateCondition(resources.NewStatus,
				appsv1alpha1.CollaSetScale, err, "UpdateFailed",
				fmt.Sprintf("fail to update Context for updating: %s", err))
			return updating, recordedRequeueAfter, err
		} else {
			collasetutils.AddOrUpdateCondition(resources.NewStatus,
				appsv1alpha1.CollaSetScale, nil, "UpdateFailed", "")
		}
	}

	// 6. update Pod
	updater := newPodUpdater(ctx, r.client, cls)
	// When PodUpdatePolicy is ReplaceUpdate, during the AnalyseAndGetUpdatedPod process, first record the original pod and the corresponding new pod to be created.
	// Then, proceed to create them in batches subsequently.
	var needReplaceOriginPods []*PodUpdateInfo
	var replacePairNewCreatePods []*corev1.Pod
	succCount, err = controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(_ int, _ error) error {
		podInfo := <-podCh
		// analyse Pod to get update information
		inPlaceSupport, onlyMetadataChanged, updatedPod, err := updater.AnalyseAndGetUpdatedPod(resources.UpdatedRevision, podInfo)
		if err != nil {
			return fmt.Errorf("fail to analyse pod %s/%s in-place update support: %s", podInfo.Namespace, podInfo.Name, err)
		}

		logger.V(1).Info("before pod update operation",
			"pod", commonutils.ObjectKeyString(podInfo.Pod),
			"revision.from", podInfo.CurrentRevision.Name,
			"revision.to", resources.UpdatedRevision.Name,
			"inPlaceUpdate", inPlaceSupport,
			"onlyMetadataChanged", onlyMetadataChanged,
		)
		if onlyMetadataChanged || inPlaceSupport {
			// 6.1 if pod template changes only include metadata or support in-place update, just apply these changes to pod directly
			if err = r.podControl.UpdatePod(updatedPod); err != nil {
				return fmt.Errorf("fail to update Pod %s/%s when updating by in-place: %s", podInfo.Namespace, podInfo.Name, err)
			} else {
				podInfo.Pod = updatedPod
				r.recorder.Eventf(podInfo.Pod,
					corev1.EventTypeNormal,
					"UpdatePod",
					"succeed to update Pod %s/%s to from revision %s to revision %s by in-place",
					podInfo.Namespace, podInfo.Name,
					podInfo.CurrentRevision.Name,
					resources.UpdatedRevision.Name)
				if err := collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, podInfo.Name, updatedPod.ResourceVersion); err != nil {
					return err
				}
			}
		} else if cls.Spec.UpdateStrategy.PodUpdatePolicy == appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType {
			if updatedPod != nil {
				needReplaceOriginPods = append(needReplaceOriginPods, podInfo)
				replacePairNewCreatePods = append(replacePairNewCreatePods, updatedPod)
			}
		} else {
			// 6.2 if pod has changes not in-place supported, recreate it
			if err = r.podControl.DeletePod(podInfo.Pod); err != nil {
				return fmt.Errorf("fail to delete Pod %s/%s when updating by recreate: %s", podInfo.Namespace, podInfo.Name, err)
			} else {
				r.recorder.Eventf(podInfo.Pod,
					corev1.EventTypeNormal,
					"UpdatePod",
					"succeed to update Pod %s/%s to from revision %s to revision %s by recreate",
					podInfo.Namespace,
					podInfo.Name,
					podInfo.CurrentRevision.Name, resources.UpdatedRevision.Name)
				if err := collasetutils.ActiveExpectations.ExpectDelete(cls, expectations.Pod, podInfo.Name); err != nil {
					return err
				}
			}
		}

		return nil
	})

	// batch create replace pair new pods
	if cls.Spec.UpdateStrategy.PodUpdatePolicy == appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType && len(needReplaceOriginPods) > 0 {
		if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			ownedIDs, err = podcontext.AllocateID(r.client, cls, resources.UpdatedRevision.Name, len(podUpdateInfos)+len(needReplaceOriginPods))
			return err
		}); err != nil {
			return false, nil, fmt.Errorf("fail to allocate %d IDs using context when create Pods for ReplaceUpdate: %s", len(needReplaceOriginPods), err)
		}
		// collect instance ID in used from owned Pods
		podInstanceIDSet := collasetutils.CollectPodInstanceID(podWrappers)
		// find IDs and their contexts which have not been used by owned Pods
		availableContext := extractAvailableContexts(len(needReplaceOriginPods), ownedIDs, podInstanceIDSet)
		succCount, err = controllerutils.SlowStartBatch(len(needReplaceOriginPods), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
			originPodInfo := needReplaceOriginPods[i]
			needCreatePod := replacePairNewCreatePods[i]
			// add instance id and replace pair label
			instanceId := fmt.Sprintf("%d", availableContext[i].ID)
			needCreatePod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = instanceId
			needCreatePod.Labels[appsv1alpha1.PodReplacePairOriginName] = originPodInfo.GetName()
			newPod := needCreatePod.DeepCopy()
			if newCreatedPod, err := r.podControl.CreatePod(newPod); err == nil {
				r.recorder.Eventf(originPodInfo.Pod,
					corev1.EventTypeNormal,
					"CreatePairPod",
					"succeed to create replace pair Pod %s/%s to from revision %s to revision %s by replace update",
					originPodInfo.Namespace,
					originPodInfo.Name,
					originPodInfo.CurrentRevision.Name, resources.UpdatedRevision.Name)
				if err := collasetutils.ActiveExpectations.ExpectCreate(cls, expectations.Pod, newCreatedPod.Name); err != nil {
					return err
				}

				patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%s"}}}`, appsv1alpha1.PodReplacePairNewId, instanceId)))
				if err = r.podControl.PatchPod(originPodInfo.Pod, patch); err != nil {
					return fmt.Errorf("fail to update origin pod %s/%s pair label %s when updating by replaceUpdate: %s", originPodInfo.Namespace, originPodInfo.Name, newCreatedPod.Name, err)
				}
			} else {
				r.recorder.Eventf(originPodInfo.Pod,
					corev1.EventTypeNormal,
					"UpdatePod",
					"failed to create replace pair Pod %s/%s to from revision %s to revision %s by replace update",
					originPodInfo.Namespace,
					originPodInfo.Name,
					originPodInfo.CurrentRevision.Name, resources.UpdatedRevision.Name)
				return err
			}
			return nil
		})
	}

	updating = updating || succCount > 0
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, err, "UpdateFailed", err.Error())
		return updating, recordedRequeueAfter, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
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
			return fmt.Errorf("failed to get pod %s/%s update finished: %s", podInfo.Namespace, podInfo.Name, err)
		}

		if finished {
			if podInfo.isInReplacing {
				replacePairNewPodInfo := podInfo.replacePairNewPodInfo
				if err = r.podControl.DeletePod(podInfo.Pod); err != nil {
					return fmt.Errorf("failed to delete origin pod %s/%s when replace finish %s/%s: %s", podInfo.Namespace, podInfo.Name, replacePairNewPodInfo.Namespace, replacePairNewPodInfo.Name, err)
				}

				if _, exist := replacePairNewPodInfo.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
					deletePatchStr := []byte(fmt.Sprintf(`[{"op": "remove", "path": "/metadata/labels/%s"}]`, strings.ReplaceAll(appsv1alpha1.PodReplacePairOriginName, "/", "~1")))
					if err = r.podControl.PatchPod(replacePairNewPodInfo.Pod, client.RawPatch(types.JSONPatchType, deletePatchStr)); err != nil {
						return fmt.Errorf("failed to remove new pod pair label when replace finish %s/%s: %s", replacePairNewPodInfo.Namespace, replacePairNewPodInfo.Name, err)
					}
				}

				logger.V(1).Info("try to finish update PodOpsLifecycle for Pod", "pod", commonutils.ObjectKeyString(podInfo.Pod))
				if updated, err := podopslifecycle.Finish(r.client, collasetutils.UpdateOpsLifecycleAdapter, replacePairNewPodInfo.Pod); err != nil {
					return fmt.Errorf("failed to finish PodOpsLifecycle for updating Pod %s/%s: %s", replacePairNewPodInfo.Namespace, replacePairNewPodInfo.Name, err)
				} else if updated {
					// add an expectation for this pod update, before next reconciling
					if err := collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, replacePairNewPodInfo.Name, replacePairNewPodInfo.ResourceVersion); err != nil {
						return err
					}
					r.recorder.Eventf(replacePairNewPodInfo.Pod,
						corev1.EventTypeNormal,
						"UpdateReady", "pod %s/%s update finished", replacePairNewPodInfo.Namespace, replacePairNewPodInfo.Name)
				}
			} else {
				logger.V(1).Info("try to finish update PodOpsLifecycle for Pod", "pod", commonutils.ObjectKeyString(podInfo.Pod))
				if updated, err := podopslifecycle.Finish(r.client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod); err != nil {
					return fmt.Errorf("failed to finish PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
				} else if updated {
					// add an expectation for this pod update, before next reconciling
					if err := collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
						return err
					}
					r.recorder.Eventf(podInfo.Pod,
						corev1.EventTypeNormal,
						"UpdateReady", "pod %s/%s update finished", podInfo.Namespace, podInfo.Name)
				}
			}
		} else {
			r.recorder.Eventf(podInfo.Pod,
				corev1.EventTypeNormal,
				"WaitingUpdateReady",
				"waiting for pod %s/%s to update finished: %s",
				podInfo.Namespace, podInfo.Name, msg)
		}

		return nil
	})

	return updating || succCount > 0, recordedRequeueAfter, err
}

func realValue(val *int32) int32 {
	if val == nil {
		return 0
	}

	return *val
}
