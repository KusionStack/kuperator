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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontext"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/operating/pkg/controllers/collaset/pvccontrol"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/features"
	commonutils "kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/feature"
)

const (
	ScaleInContextDataKey            = "ScaleIn"
	ReplaceNewPodIDContextDataKey    = "ReplaceNewPodID"
	ReplaceOriginPodIDContextDataKey = "ReplaceOriginPodID"
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

	needReplaceOriginPods, needCleanLabelPods, podsNeedCleanLabels, needDeletePods := dealReplacePods(filteredPods, instance)
	if err := r.deletePodsByLabel(needDeletePods); err != nil {
		r.recorder.Eventf(instance, corev1.EventTypeWarning, "ReplacePod", "delete pods by label with error: %s", err.Error())
	}

	// get owned IDs
	var ownedIDs map[int]*appsv1alpha1.ContextDetail
	if err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		needAllocateReplicas := maxInt(len(filteredPods), int(realValue(instance.Spec.Replicas))) + len(needReplaceOriginPods)
		// TODO choose revision according to scaleStrategy
		ownedIDs, err = podcontext.AllocateID(r.client, instance, resources.UpdatedRevision.Name, needAllocateReplicas)
		return err
	}); err != nil {
		return false, nil, ownedIDs, fmt.Errorf("fail to allocate %d IDs using context when sync Pods: %s", instance.Spec.Replicas, err)
	}

	// wrap Pod with more information
	var podWrappers []*collasetutils.PodWrapper

	// stateless case
	currentIDs := make(map[int]struct{})
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

			// 2. filter out Pods which are terminating
			continue
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
		})

		if id >= 0 {
			currentIDs[id] = struct{}{}
		}
	}

	needUpdateContext := false
	needDeleteOriginPodsIDs := sets.String{}
	if len(needCleanLabelPods) > 0 {
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
				// replace finished, (1) remove ReplaceOriginPodID from new pod ID, (2) delete origin Pod's ID
				if labelKey == appsv1alpha1.PodReplacePairOriginName {
					needUpdateContext = true
					id, _ := collasetutils.GetPodInstanceID(pod)
					if val, exist := ownedIDs[id].Data[ReplaceOriginPodIDContextDataKey]; exist {
						needDeleteOriginPodsIDs.Insert(val)
						ownedIDs[id].Remove(ReplaceOriginPodIDContextDataKey)
					}
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
		if err != nil {
			r.recorder.Eventf(instance, corev1.EventTypeWarning, "ReplacePod", "clean pods replace pair origin name label with error: %s", err.Error())
		}
	}

	// 3. Reclaim Pod ID which is (1) during ScalingIn, (2) ReplaceOriginPod; besides, Pod & PVC are all non-existing
	for id, contextDetail := range ownedIDs {
		if _, exist := currentIDs[id]; exist {
			continue
		}
		_, exist := needDeleteOriginPodsIDs[strconv.Itoa(contextDetail.ID)]
		if contextDetail.Contains(ScaleInContextDataKey, "true") || exist {
			idToReclaim.Insert(id)
		}
	}

	// 4. create new pods for need replace pods
	if len(needReplaceOriginPods) > 0 {
		availableContexts := extractAvailableContexts(len(needReplaceOriginPods), ownedIDs, currentIDs)
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
			instanceId := fmt.Sprintf("%d", availableContexts[i].ID)
			if val, exist := ownedIDs[originPodId].Data[ReplaceNewPodIDContextDataKey]; exist {
				// reuse new pod ID if pair-relation exists
				id, _ := strconv.ParseInt(val, 10, 32)
				if ownedIDs[int(id)] != nil {
					instanceId = val
				}
			}
			newPod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = instanceId
			newPod.Labels[appsv1alpha1.PodReplacePairOriginName] = originPod.GetName()
			// add replace pair-relation to IDs for originPod and newPod
			newPodId, _ := collasetutils.GetPodInstanceID(newPod)
			ownedIDs[originPodId].Put(ReplaceNewPodIDContextDataKey, strconv.Itoa(newPodId))
			ownedIDs[newPodId].Put(ReplaceOriginPodIDContextDataKey, strconv.Itoa(originPodId))
			// create pvcs for new pod
			err = r.pvcControl.CreatePodPvcs(ctx, instance, newPod, resources.ExistingPvcs)
			if err != nil {
				return fmt.Errorf("fail to migrate PVCs from origin pod %s to replace pod %s: %s", originPod.Name, newPod.Name, err)
			}
			if newCreatedPod, err := r.podControl.CreatePod(newPod); err == nil {
				availableContexts[i].Put(podcontext.RevisionContextDataKey, replaceRevision.Name)
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

		if err != nil {
			r.recorder.Eventf(instance, corev1.EventTypeWarning, "ReplacePod", "deal replace pods with error: %s", err.Error())
		}

		if successCount > 0 {
			needUpdateContext = true
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

	return false, podWrappers, ownedIDs, nil
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

func dealReplacePods(pods []*corev1.Pod, instance *appsv1alpha1.CollaSet) (needReplacePods []*corev1.Pod, needCleanLabelPods []*corev1.Pod, podNeedCleanLabels [][]string, needDeletePods []*corev1.Pod) {
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
	replacePodMap := classifyPodReplacingMapping(podWrappers)

	diff := int(realValue(cls.Spec.Replicas)) - len(replacePodMap)
	scaling := false

	if diff >= 0 {
		// trigger delete pods indicated in ScaleStrategy.PodToDelete by label
		for _, podWrapper := range podWrappers {
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
			podInstanceIDSet := collasetutils.CollectPodInstanceID(podWrappers)
			// find IDs and their contexts which have not been used by owned Pods
			availableContext := extractAvailableContexts(diff, ownedIDs, podInstanceIDSet)
			succCount, err := controllerutils.SlowStartBatch(diff, controllerutils.SlowStartInitialBatchSize, false, func(idx int, _ error) (err error) {
				availableIDContext := availableContext[idx]
				defer func() {
					decideContextRevision(availableIDContext, resources.CurrentRevision, resources.UpdatedRevision, err == nil)
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
					metav1.NewControllerRef(cls, appsv1alpha1.GroupVersion.WithKind("CollaSet")),
					revision,
					func(in *corev1.Pod) (localErr error) {
						in.Labels[appsv1alpha1.PodInstanceIDLabelKey] = fmt.Sprintf("%d", availableIDContext.ID)
						revisionsInfo, ok := availableIDContext.Get(podcontext.PodDecorationRevisionKey)
						var pds map[string]*appsv1alpha1.PodDecoration
						if !ok {
							// get default PodDecorations if no revision in context
							pds, localErr = resources.PDGetter.GetEffective(ctx, in)
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
			if err != nil || succCount > 0 {
				logger.V(1).Info("try to update ResourceContext for CollaSet after scaling out")
				if updateContextErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					return podcontext.UpdateToPodContext(r.client, cls, ownedIDs)
				}); updateContextErr != nil {
					err = controllerutils.AggregateErrors([]error{updateContextErr, err})
				}
			}
			r.recorder.Eventf(cls, corev1.EventTypeNormal, "ScaleOut", "scale out %d Pod(s)", succCount)
			if err != nil {
				collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, err, "ScaleOutFailed", err.Error())
				return succCount > 0, recordedRequeueAfter, err
			}
			collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetScale, nil, "ScaleOut", "")
			return succCount > 0, recordedRequeueAfter, err
		}
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

// decide revision: (1) just create, (2) upgrade, (3) delete
func decideContextRevision(contextDetail *appsv1alpha1.ContextDetail, currentRevision, updatedRevision *appsv1.ControllerRevision, createSucceeded bool) {
	if !createSucceeded {
		if contextDetail.Contains(podcontext.PodJustCreateContextDataKey, "true") {
			// TODO choose revision according to scaleStrategy
			contextDetail.Put(podcontext.RevisionContextDataKey, currentRevision.Name)
		} else if contextDetail.Contains(podcontext.PodUpgradeContextDataKey, "true") {
			contextDetail.Put(podcontext.RevisionContextDataKey, updatedRevision.Name)
		}
		// if delete, don't change revisionKey
	} else {
		// TODO delete ID if create succeeded
		contextDetail.Remove(podcontext.PodJustCreateContextDataKey)
		contextDetail.Remove(podcontext.PodUpgradeContextDataKey)
	}
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
	podUpdateInfos, err := attachPodUpdateInfo(ctx, cls, podWrappers, resources)
	if err != nil {
		return false, nil, fmt.Errorf("fail to attach pod update info, %v", err)
	}

	// 2. decide Pod update candidates
	podToUpdate := decidePodToUpdate(cls, podUpdateInfos, ownedIDs, resources.UpdatedRevision)
	podCh := make(chan *PodUpdateInfo, len(podToUpdate))
	updater := newPodUpdater(ctx, r.client, cls, r.podControl, r.recorder)
	updating := false

	// 3. filter already updated revision,
	for i, podInfo := range podToUpdate {
		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		// 3.1 fulfillPodUpdateInfo to all not updatedRevision pod
		if err = updater.FulfillPodUpdatedInfo(resources.UpdatedRevision, podInfo); err != nil {
			logger.Error(err, fmt.Sprintf("fail to analyse pod %s/%s in-place update support", podInfo.Namespace, podInfo.Name))
			continue
		}

		if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, podInfo) {
			continue
		}

		podCh <- podToUpdate[i]
	}

	// 4. begin pod update lifecycle
	updating, err = updater.BeginUpdatePod(resources, podCh)
	if err != nil {
		return updating, recordedRequeueAfter, err
	}

	// 5. filter pods not allow to ops now, such as OperationDelaySeconds strategy
	recordedRequeueAfter, err = updater.FilterAllowOpsPods(podToUpdate, ownedIDs, resources, podCh)
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetScale, err, "UpdateFailed",
			fmt.Sprintf("fail to update Context for updating: %s", err))
		return updating, recordedRequeueAfter, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetScale, nil, "Updated", "")
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

		if err = updater.UpgradePod(podInfo); err != nil {
			return err
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

	// 7. try to finish all Pods'PodOpsLifecycle if its update is finished.
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
			err := updater.FinishUpdatePod(podInfo)
			if err != nil {
				return err
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
