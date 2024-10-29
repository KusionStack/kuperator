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
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/collaset/podcontext"
	"kusionstack.io/kuperator/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/kuperator/pkg/controllers/collaset/pvccontrol"
	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	ojutils "kusionstack.io/kuperator/pkg/controllers/operationjob/utils"
	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/kuperator/pkg/controllers/utils/poddecoration"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/anno"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/features"
	commonutils "kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/feature"
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

func NewRealSyncControl(client client.Client, logger logr.Logger, podControl podcontrol.Interface, pvcControl pvccontrol.Interface, recorder record.EventRecorder) Interface {
	return &RealSyncControl{
		client:     client,
		logger:     logger,
		podControl: podControl,
		pvcControl: pvcControl,
		recorder:   recorder,
	}
}

var _ Interface = &RealSyncControl{}

type RealSyncControl struct {
	client     client.Client
	logger     logr.Logger
	podControl podcontrol.Interface
	pvcControl pvccontrol.Interface
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

	// list pvcs using ownerReference
	if resources.ExistingPvcs, err = r.pvcControl.GetFilteredPvcs(ctx, instance); err != nil {
		return false, nil, nil, fmt.Errorf("fail to get filtered PVCs: %s", err)
	}
	// adopt and retain orphaned pvcs according to PVC retention policy
	if adoptedPvcs, err := r.pvcControl.AdoptOrphanedPvcs(ctx, instance); err != nil {
		return false, nil, nil, fmt.Errorf("fail to adopt orphaned PVCs: %s", err)
	} else {
		resources.ExistingPvcs = append(resources.ExistingPvcs, adoptedPvcs...)
	}

	needReplaceOriginPods, needCleanLabelPods, podsNeedCleanLabels, needDeletePods, replaceIndicateCount := dealReplacePods(filteredPods)

	// get owned IDs
	var ownedIDs map[int]*appsv1alpha1.ContextDetail
	if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		needAllocateReplicas := int(realValue(instance.Spec.Replicas)) + replaceIndicateCount
		ownedIDs, err = podcontext.AllocateID(r.client, instance, resources.UpdatedRevision.Name, needAllocateReplicas)
		return err
	}); err != nil {
		return false, nil, ownedIDs, fmt.Errorf("fail to allocate %d IDs using context when sync Pods: %s", instance.Spec.Replicas, err)
	}

	// wrap Pod with more information
	var podWrappers []*collasetutils.PodWrapper

	// stateless case
	currentIDs := make(map[int]struct{})
	needUpdateContext := false
	idToReclaim := sets.Int{}
	toDeletePodNames := sets.NewString(instance.Spec.ScaleStrategy.PodToDelete...)
	for i := range filteredPods {
		pod := filteredPods[i]
		id, _ := collasetutils.GetPodInstanceID(pod)
		toDelete := toDeletePodNames.Has(pod.Name)

		if toDelete {
			toDeletePodNames.Delete(pod.Name)
		}

		if pod.DeletionTimestamp != nil {
			// 1. Reclaim ID from Pod which is scaling in and terminating.
			if contextDetail, exist := ownedIDs[id]; exist && contextDetail.Contains(ScaleInContextDataKey, "true") {
				idToReclaim.Insert(id)
			}

			_, replaceIndicate := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			// 2. filter out Pods which are terminating and not replace indicate
			if !replaceIndicate {
				continue
			}
		}

		// delete unused pvcs
		if err := r.pvcControl.DeletePodUnusedPvcs(ctx, instance, pod, resources.ExistingPvcs); err != nil {
			return false, nil, nil, fmt.Errorf("fail to delete unused pvcs %s", err)
		}

		podWrappers = append(podWrappers, &collasetutils.PodWrapper{
			Pod:           pod,
			ID:            id,
			ContextDetail: ownedIDs[id],
			ToDelete:      toDelete,
			PlaceHolder:   false,
		})

		if id >= 0 {
			currentIDs[id] = struct{}{}
		}
	}

	// 3.1 delete origin pods for replace
	if len(needDeletePods) > 0 {
		if err := r.deletePodsByLabel(needDeletePods); err != nil {
			r.recorder.Eventf(instance, corev1.EventTypeWarning, "ReplacePod", "delete pods by label with error: %s", err.Error())
		}

	}

	// 3.2 clean labels for replace pods
	needDeletePodsIDs := sets.String{}
	if len(needCleanLabelPods) > 0 {
		needUpdateContext, needDeletePodsIDs, err = r.cleanReplacePodLabels(needCleanLabelPods, podsNeedCleanLabels, ownedIDs)
		if err != nil {
			r.recorder.Eventf(instance, corev1.EventTypeWarning, "ReplacePod", "clean pods replace pair origin name label with error: %s", err.Error())
		}
	}

	// 3.3 create new pods for need replace pods
	if len(needReplaceOriginPods) > 0 {
		successCount, err := r.replaceOriginPods(ctx, instance, resources, needReplaceOriginPods, ownedIDs, currentIDs)

		if err != nil {
			r.recorder.Eventf(instance, corev1.EventTypeWarning, "ReplacePod", "deal replace pods with error: %s", err.Error())
		}

		if successCount > 0 {
			needUpdateContext = true
		}
	}

	// 4. Reclaim Pod ID which is (1) during ScalingIn, (2) ReplaceOriginPod; besides, Pod & PVC are all non-existing
	for id, contextDetail := range ownedIDs {
		if _, exist := currentIDs[id]; exist {
			continue
		}
		_, exist := needDeletePodsIDs[strconv.Itoa(contextDetail.ID)]
		if contextDetail.Contains(ScaleInContextDataKey, "true") || exist {
			idToReclaim.Insert(id)
		}
	}

	// 5. Reclaim podToDelete if necessary
	// ReclaimPodToDelete FeatureGate defaults to true
	// Add '--feature-gates=ReclaimPodToDelete=false' to container args, to disable reclaim of podToDelete
	if feature.DefaultFeatureGate.Enabled(features.ReclaimPodToDelete) && len(toDeletePodNames) > 0 {
		var newPodToDelete []string
		for _, podName := range instance.Spec.ScaleStrategy.PodToDelete {
			if !toDeletePodNames.Has(podName) {
				newPodToDelete = append(newPodToDelete, podName)
			}
		}
		// update cls.spec.scaleStrategy.podToDelete
		instance.Spec.ScaleStrategy.PodToDelete = newPodToDelete
		if err := r.updateCollaSet(ctx, instance); err != nil {
			return false, nil, ownedIDs, err
		}
	}

	// TODO stateful case
	// 1. only reclaim non-existing Pods' ID. Do not reclaim terminating Pods' ID until these Pods and PVC have been deleted from ETCD
	// 2. do not filter out these terminating Pods

	for _, id := range idToReclaim.List() {
		needUpdateContext = true
		delete(ownedIDs, id)
	}

	if needUpdateContext {
		logger.V(1).Info("try to update ResourceContext for CollaSet when sync")
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(r.client, instance, ownedIDs)
		}); err != nil {
			return false, nil, ownedIDs, fmt.Errorf("fail to update ResourceContext when reclaiming IDs: %s", err)
		}
	}

	// 6. create podWrappers for non-exist pods
	for id, contextDetail := range ownedIDs {
		if _, inUsed := currentIDs[id]; inUsed {
			continue
		}
		podWrappers = append(podWrappers, &collasetutils.PodWrapper{
			ID:            id,
			Pod:           nil,
			ContextDetail: contextDetail,
			PlaceHolder:   true,
		})
	}

	return false, podWrappers, ownedIDs, nil
}

func (r *RealSyncControl) updateCollaSet(ctx context.Context, cls *appsv1alpha1.CollaSet) error {
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return r.client.Update(ctx, cls)
	}); err != nil {
		return err
	}
	return collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.CollaSet, cls.Name, cls.ResourceVersion)
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
	activePods := FilterOutPlaceHolderPodWrappers(podWrappers)
	replacePodMap := classifyPodReplacingMapping(activePods)

	diff := int(realValue(cls.Spec.Replicas)) - len(replacePodMap)
	scaling := false

	if diff >= 0 {
		// trigger delete pods indicated in ScaleStrategy.PodToDelete by label
		for _, podWrapper := range activePods {
			if podWrapper.ToDelete {
				err := r.deletePodsByLabel([]*corev1.Pod{podWrapper.Pod})
				if err != nil {
					return false, recordedRequeueAfter, err
				}
			}
		}

		// scale out pods and return if diff > 0
		if diff > 0 {
			// collect instance ID in used from owned Pods
			podInstanceIDSet := collasetutils.CollectPodInstanceID(activePods)
			// find IDs and their contexts which have not been used by owned Pods
			availableContext := extractAvailableContexts(diff, ownedIDs, podInstanceIDSet)
			needUpdateContext := atomic.Bool{}
			succCount, err := controllerutils.SlowStartBatch(diff, controllerutils.SlowStartInitialBatchSize, false, func(idx int, _ error) (err error) {
				availableIDContext := availableContext[idx]
				defer func() {
					if decideContextRevision(availableIDContext, resources.UpdatedRevision, err == nil) {
						needUpdateContext.Store(true)
					}
				}()
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
					metav1.NewControllerRef(cls, appsv1alpha1.SchemeGroupVersion.WithKind("CollaSet")),
					revision,
					func(in *corev1.Pod) (localErr error) {
						in.Labels[appsv1alpha1.PodInstanceIDLabelKey] = fmt.Sprintf("%d", availableIDContext.ID)
						if availableIDContext.Data[podcontext.JustCreateContextDataKey] == "true" {
							in.Labels[appsv1alpha1.PodCreatingLabel] = strconv.FormatInt(time.Now().UnixNano(), 10)
						} else {
							in.Labels[appsv1alpha1.PodCompletingLabel] = strconv.FormatInt(time.Now().UnixNano(), 10)
						}
						revisionsInfo, ok := availableIDContext.Get(podcontext.PodDecorationRevisionKey)
						var pds map[string]*appsv1alpha1.PodDecoration
						if !ok {
							// get default PodDecorations if no revision in context
							pds, localErr = resources.PDGetter.GetEffective(ctx, in)
							if localErr != nil {
								return localErr
							}
							needUpdateContext.Store(true)
							availableIDContext.Put(podcontext.PodDecorationRevisionKey, anno.GetDecorationInfoString(pds))
						} else {
							// upgrade by recreate pod case
							infos, marshallErr := anno.UnmarshallFromString(revisionsInfo)
							if marshallErr != nil {
								return marshallErr
							}
							var revisions []string
							for _, info := range infos {
								revisions = append(revisions, info.Revision)
							}
							pds, localErr = resources.PDGetter.GetByRevisions(ctx, revisions...)
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
				err = r.pvcControl.CreatePodPvcs(ctx, cls, pod, resources.ExistingPvcs)
				if err != nil {
					return fmt.Errorf("fail to create PVCs for pod %s: %s", pod.Name, err)
				}
				newPod := pod.DeepCopy()
				logger.V(1).Info("try to create Pod with revision of collaSet", "revision", revision.Name)
				if pod, err = r.podControl.CreatePod(newPod); err != nil {
					return err
				}
				// add an expectation for this pod creation, before next reconciling
				return collasetutils.ActiveExpectations.ExpectCreate(cls, expectations.Pod, pod.Name)
			})
			if needUpdateContext.Load() {
				logger.V(1).Info("try to update ResourceContext for CollaSet after scaling out")
				if updateContextErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					return podcontext.UpdateToPodContext(r.client, cls, ownedIDs)
				}); updateContextErr != nil {
					err = controllerutils.AggregateErrors([]error{updateContextErr, err})
				}
			}
			if err != nil {
				collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, err, "ScaleOutFailed", err.Error())
				return succCount > 0, recordedRequeueAfter, err
			}
			r.recorder.Eventf(cls, corev1.EventTypeNormal, "ScaleOut", "scale out %d Pod(s)", succCount)
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, nil, "ScaleOut", "")
			return succCount > 0, recordedRequeueAfter, err
		}
	} else if diff < 0 {
		// chose the pods to scale in
		podsToScaleIn := getPodsToDelete(activePods, replacePodMap, diff*-1)
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
			if contextDetail, exist := ownedIDs[podWrapper.ID]; exist && !contextDetail.Contains(ScaleInContextDataKey, "true") {
				needUpdateContext = true
				contextDetail.Put(ScaleInContextDataKey, "true")
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

			// delete PVC if pod is in update replace, or retention policy is "Deleted"
			_, originExist := pod.Labels[appsv1alpha1.PodReplacePairNewId]
			_, replaceExist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]
			if originExist || replaceExist || collasetutils.PvcPolicyWhenScaled(cls) == appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType {
				return r.pvcControl.DeletePodPvcs(ctx, cls, pod.Pod, resources.ExistingPvcs)
			}
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
	for _, podWrapper := range activePods {
		if contextDetail, exist := ownedIDs[podWrapper.ID]; exist && contextDetail.Contains(ScaleInContextDataKey, "true") &&
			!podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, podWrapper) {
			needUpdatePodContext = true
			contextDetail.Remove(ScaleInContextDataKey)
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

func FilterOutPlaceHolderPodWrappers(pods []*collasetutils.PodWrapper) []*collasetutils.PodWrapper {
	var filteredPodWrappers []*collasetutils.PodWrapper
	for _, pod := range pods {
		if pod.PlaceHolder {
			continue
		}
		filteredPodWrappers = append(filteredPodWrappers, pod)
	}
	return filteredPodWrappers
}

func extractAvailableContexts(diff int, ownedIDs map[int]*appsv1alpha1.ContextDetail, podInstanceIDSet map[int]struct{}) []*appsv1alpha1.ContextDetail {
	availableContexts := make([]*appsv1alpha1.ContextDetail, diff)

	idx := 0
	for id := range ownedIDs {
		if _, inUsed := podInstanceIDSet[id]; inUsed {
			continue
		}

		// skip replaceNewPod ID
		if ownedIDs[id].Data[ReplaceOriginPodIDContextDataKey] != "" {
			continue
		}

		availableContexts[idx] = ownedIDs[id]
		idx++
		if idx == diff {
			break
		}
	}

	return availableContexts
}

func (r *RealSyncControl) deletePodsByLabel(needDeletePods []*corev1.Pod) error {
	_, err := controllerutils.SlowStartBatch(len(needDeletePods), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		pod := needDeletePods[i]
		if _, exist := pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; !exist {
			patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%d"}}}`, appsv1alpha1.PodDeletionIndicationLabelKey, time.Now().UnixNano())))
			if err := r.podControl.PatchPod(pod, patch); err != nil {
				return fmt.Errorf("failed to delete pod when syncPods %s/%s %s", pod.Namespace, pod.Name, err)
			}
		}
		return nil
	})
	return err
}

// decide revision for 3 pod create types: (1) just create, (2) upgrade by recreate, (3) delete and recreate
func decideContextRevision(contextDetail *appsv1alpha1.ContextDetail, updatedRevision *appsv1.ControllerRevision, createSucceeded bool) bool {
	needUpdateContext := false
	if !createSucceeded {
		if contextDetail.Contains(podcontext.JustCreateContextDataKey, "true") {
			// TODO choose just create pods' revision according to scaleStrategy
			contextDetail.Put(podcontext.RevisionContextDataKey, updatedRevision.Name)
			delete(contextDetail.Data, podcontext.PodDecorationRevisionKey)
			needUpdateContext = true
		} else if contextDetail.Contains(podcontext.RecreateUpdateContextDataKey, "true") {
			contextDetail.Put(podcontext.RevisionContextDataKey, updatedRevision.Name)
			delete(contextDetail.Data, podcontext.PodDecorationRevisionKey)
			needUpdateContext = true
		}
		// if pod is delete and recreate, never change revisionKey
	} else {
		// TODO delete ID if create succeeded
		contextDetail.Remove(podcontext.JustCreateContextDataKey)
		contextDetail.Remove(podcontext.RecreateUpdateContextDataKey)
		needUpdateContext = true
	}
	return needUpdateContext
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
	// 1. scan and analysis pods update info for active pods and PlaceHolder pods
	podUpdateInfos, err := attachPodUpdateInfo(ctx, cls, podWrappers, resources)
	if err != nil {
		return false, nil, fmt.Errorf("fail to attach pod update info, %v", err)
	}

	// 2. decide Pod update candidates
	candidates := decidePodToUpdate(cls, podUpdateInfos)
	podToUpdate := filterOutPlaceHolderUpdateInfos(candidates)
	podCh := make(chan *PodUpdateInfo, len(podToUpdate))
	updater := newPodUpdater(r.client, cls, r.podControl, r.recorder)
	updating := false

	// 3. filter already updated revision,
	for i, podInfo := range podToUpdate {
		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		// 3.1 fulfillPodUpdateInfo to all not updatedRevision pod
		if err = updater.FulfillPodUpdatedInfo(ctx, resources.UpdatedRevision, podInfo); err != nil {
			logger.Error(err, fmt.Sprintf("fail to analyse pod %s/%s in-place update support", podInfo.Namespace, podInfo.Name))
			continue
		}

		if podInfo.DeletionTimestamp != nil {
			continue
		}

		if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, podInfo) {
			continue
		}

		podCh <- podToUpdate[i]
	}

	// 4. begin pod update lifecycle
	updating, err = updater.BeginUpdatePod(ctx, resources, podCh)
	if err != nil {
		return updating, recordedRequeueAfter, err
	}

	// 5. (1) filter out  pods not allow to ops now, such as OperationDelaySeconds strategy; (2) update PlaceHolder Pods resourceContext revision
	recordedRequeueAfter, err = updater.FilterAllowOpsPods(ctx, candidates, ownedIDs, resources, podCh)
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetUpdate, err, "UpdateFailed",
			fmt.Sprintf("fail to update Context for updating: %s", err))
		return updating, recordedRequeueAfter, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}

	// 6. update Pod
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(_ int, _ error) error {
		podInfo := <-podCh
		logger.V(1).Info("before pod update operation",
			"pod", commonutils.ObjectKeyString(podInfo.Pod),
			"revision.from", podInfo.CurrentRevision.Name,
			"revision.to", resources.UpdatedRevision.Name,
			"inPlaceUpdate", podInfo.InPlaceUpdateSupport,
			"onlyMetadataChanged", podInfo.OnlyMetadataChanged,
		)

		isReplaceUpdate := cls.Spec.UpdateStrategy.PodUpdatePolicy == appsv1alpha1.CollaSetReplacePodUpdateStrategyType
		if podInfo.isInReplacing && !isReplaceUpdate {
			// a replacing pod should be replaced by an updated revision pod when encountering upgrade
			if err = updateReplaceOriginPod(ctx, r.client, r.recorder, podInfo, podInfo.replacePairNewPodInfo); err != nil {
				return err
			}
		} else {
			if err = updater.UpgradePod(ctx, podInfo); err != nil {
				return err
			}
		}

		return nil
	})

	updating = updating || succCount > 0
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, err, "UpdateFailed", err.Error())
		return updating, recordedRequeueAfter, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}

	podToUpdateSet := sets.String{}
	for i := range podToUpdate {
		podToUpdateSet.Insert(podToUpdate[i].Name)
	}
	// 7. try to finish all Pods'PodOpsLifecycle if its update is finished.
	succCount, err = controllerutils.SlowStartBatch(len(podUpdateInfos), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		podInfo := podUpdateInfos[i]

		if !podInfo.isDuringOps || podInfo.PlaceHolder || podInfo.DeletionTimestamp != nil {
			return nil
		}

		if !podInfo.isAllowOps {
			// pod is not included by podToUpdate, not allowOps, but isDuringOps, just cancel
			if !podToUpdateSet.Has(podInfo.Name) {
				r.recorder.Eventf(podInfo.Pod,
					corev1.EventTypeNormal,
					"UpdatePodCanceled",
					"pod %s/%s update is canceled due to not started and not included partition %s",
					podInfo.Namespace, podInfo.Name, podInfo.CurrentRevision.Name)
				return ojutils.CancelOpsLifecycle(ctx, r.client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod)
			}
			// not allowedOps, skip GetPodUpdateFinishStatus
			return nil
		}

		// check Pod is during updating, and it is finished or not
		finished, msg, err := updater.GetPodUpdateFinishStatus(ctx, podInfo)
		if err != nil {
			return fmt.Errorf("failed to get pod %s/%s update finished: %s", podInfo.Namespace, podInfo.Name, err)
		}

		if finished {
			if err := updater.FinishUpdatePod(ctx, podInfo); err != nil {
				return err
			}
			r.recorder.Eventf(podInfo.Pod,
				corev1.EventTypeNormal,
				"UpdatePodFinished",
				"pod %s/%s is finished for upgrade to revision %s",
				podInfo.Namespace, podInfo.Name, podInfo.CurrentRevision.Name)
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
