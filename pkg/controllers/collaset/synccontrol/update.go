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
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/collaset/podcontext"
	"kusionstack.io/kuperator/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/kuperator/pkg/controllers/collaset/pvccontrol"
	"kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	ojutils "kusionstack.io/kuperator/pkg/controllers/operationjob/utils"
	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/kuperator/pkg/controllers/utils/poddecoration"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/anno"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
)

const UnknownRevision = "__unknownRevision__"

type PodUpdateInfo struct {
	*utils.PodWrapper

	UpdatedPod *corev1.Pod

	InPlaceUpdateSupport bool
	OnlyMetadataChanged  bool

	// indicate if this pod has up-to-date revision from its owner, like CollaSet
	IsUpdatedRevision bool
	// carry the pod's current revision
	CurrentRevision *appsv1.ControllerRevision
	// carry the desired update revision
	UpdateRevision *appsv1.ControllerRevision

	// indicates effected PodDecorations changed
	PodDecorationChanged bool
	// indicate if the pvc template changed
	PvcTmpHashChanged bool

	CurrentPodDecorations map[string]*appsv1alpha1.PodDecoration
	UpdatedPodDecorations map[string]*appsv1alpha1.PodDecoration

	// indicates the Pod is during UpdateOpsLifecycle
	isDuringUpdateOps bool
	// indicates the Pod is during ScaleOpsLifecycle
	isDuringScaleInOps bool
	// indicates operate is allowed for Update
	isAllowUpdateOps bool
	// requeue after for operationDelaySeconds
	requeueForOperationDelay *time.Duration

	// for replace update
	// judge pod in replace and replace updating
	isInReplace       bool
	isInReplaceUpdate bool

	// replace new created pod
	replacePairNewPodInfo *PodUpdateInfo

	// replace origin pod
	replacePairOriginPodName string
}

func (r *RealSyncControl) attachPodUpdateInfo(ctx context.Context, cls *appsv1alpha1.CollaSet, pods []*collasetutils.PodWrapper, resource *collasetutils.RelatedResources) ([]*PodUpdateInfo, error) {
	activePods := FilterOutPlaceHolderPodWrappers(pods)
	podUpdateInfoList := make([]*PodUpdateInfo, len(activePods))

	for i, pod := range activePods {
		updateInfo := &PodUpdateInfo{
			PodWrapper: pod,
		}

		currentPDs, err := resource.PDGetter.GetOnPod(ctx, pod.Pod)
		if err != nil {
			return nil, err
		}
		updatedPDs, err := resource.PDGetter.GetEffective(ctx, pod.Pod)
		if err != nil {
			return nil, err
		}

		if len(currentPDs) != len(updatedPDs) {
			updateInfo.PodDecorationChanged = true
		} else {
			revisionSets := sets.NewString()
			for rev := range currentPDs {
				revisionSets.Insert(rev)
			}
			for rev := range updatedPDs {
				if !revisionSets.Has(rev) {
					updateInfo.PodDecorationChanged = true
					break
				}
			}
		}
		updateInfo.CurrentPodDecorations = currentPDs
		updateInfo.UpdatedPodDecorations = updatedPDs

		updateInfo.UpdateRevision = resource.UpdatedRevision
		// decide this pod current revision, or nil if not indicated
		if pod.Labels != nil {
			currentRevisionName, exist := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
			if exist {
				if currentRevisionName == resource.UpdatedRevision.Name {
					updateInfo.IsUpdatedRevision = true
					updateInfo.CurrentRevision = resource.UpdatedRevision
				} else {
					updateInfo.IsUpdatedRevision = false
					for _, rv := range resource.Revisions {
						if currentRevisionName == rv.Name {
							updateInfo.CurrentRevision = rv
						}
					}
				}
			}
		}

		// default CurrentRevision is an empty revision
		if updateInfo.CurrentRevision == nil {
			updateInfo.CurrentRevision = &appsv1.ControllerRevision{
				ObjectMeta: metav1.ObjectMeta{
					Name: UnknownRevision,
				},
			}
			r.recorder.Eventf(pod.Pod,
				corev1.EventTypeWarning,
				"PodCurrentRevisionNotFound",
				"pod is going to be updated by recreate because: (1) controller-revision-hash label not found, or (2) not found in history revisions")
		}

		// decide whether the PodOpsLifecycle is during ops or not
		updateInfo.isDuringUpdateOps = podopslifecycle.IsDuringOps(utils.UpdateOpsLifecycleAdapter, pod)
		updateInfo.isDuringScaleInOps = podopslifecycle.IsDuringOps(utils.ScaleInOpsLifecycleAdapter, pod)
		updateInfo.requeueForOperationDelay, updateInfo.isAllowUpdateOps = podopslifecycle.AllowOps(collasetutils.UpdateOpsLifecycleAdapter, realValue(cls.Spec.UpdateStrategy.OperationDelaySeconds), pod)
		updateInfo.PvcTmpHashChanged, err = pvccontrol.IsPodPvcTmpChanged(cls, pod.Pod, resource.ExistingPvcs)
		if err != nil {
			return nil, fmt.Errorf("fail to check pvc template changed, %v", err)
		}
		podUpdateInfoList[i] = updateInfo
	}

	// attach replace info
	var podUpdateInfoMap = make(map[string]*PodUpdateInfo)
	for _, podUpdateInfo := range podUpdateInfoList {
		podUpdateInfoMap[podUpdateInfo.Name] = podUpdateInfo
	}
	replacePodMap := classifyPodReplacingMapping(activePods)
	// originPod's isAllowUpdateOps depends on these 2 cases:
	// (1) pod is during replacing but not during replaceUpdate, keep it legacy value
	// (2) pod is during replaceUpdate, set to "true" if newPod is service available
	for originPodName, replacePairNewPod := range replacePodMap {
		originPodInfo := podUpdateInfoMap[originPodName]
		_, replaceIndicated := originPodInfo.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
		_, replaceByReplaceUpdate := originPodInfo.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
		isReplaceUpdating := replaceIndicated && replaceByReplaceUpdate

		originPodInfo.isInReplace = replaceIndicated
		originPodInfo.isInReplaceUpdate = isReplaceUpdating
		if replacePairNewPod != nil {
			// origin pod is allowed to ops if new pod is serviceAvailable
			_, newPodSa := replacePairNewPod.Labels[appsv1alpha1.PodServiceAvailableLabel]
			originPodInfo.isAllowUpdateOps = originPodInfo.isAllowUpdateOps || newPodSa
			// attach replace new pod updateInfo
			replacePairNewPodInfo := podUpdateInfoMap[replacePairNewPod.Name]
			replacePairNewPodInfo.isInReplace = true
			// in case of to-replace label is removed from origin pod, new pod is still in replaceUpdate
			replacePairNewPodInfo.isInReplaceUpdate = replaceByReplaceUpdate

			replacePairNewPodInfo.replacePairOriginPodName = originPodName
			originPodInfo.replacePairNewPodInfo = replacePairNewPodInfo
		}
	}

	// join PlaceHolder pods in updating
	for _, pod := range pods {
		if !pod.PlaceHolder {
			continue
		}
		updateInfo := &PodUpdateInfo{
			PodWrapper:     pod,
			UpdateRevision: resource.UpdatedRevision,
		}
		if revision, exist := pod.ContextDetail.Data[podcontext.RevisionContextDataKey]; exist &&
			revision == resource.UpdatedRevision.Name {
			updateInfo.IsUpdatedRevision = true
		}
		podUpdateInfoList = append(podUpdateInfoList, updateInfo)
	}

	return podUpdateInfoList, nil
}

func filterOutPlaceHolderUpdateInfos(pods []*PodUpdateInfo) []*PodUpdateInfo {
	var filteredPodUpdateInfos []*PodUpdateInfo
	for _, pod := range pods {
		if pod.PlaceHolder {
			continue
		}
		filteredPodUpdateInfos = append(filteredPodUpdateInfos, pod)
	}
	return filteredPodUpdateInfos
}

func decidePodToUpdate(
	cls *appsv1alpha1.CollaSet,
	podInfos []*PodUpdateInfo) []*PodUpdateInfo {
	filteredPodInfos := getTargetsUpdatePods(podInfos)

	if cls.Spec.UpdateStrategy.RollingUpdate != nil && cls.Spec.UpdateStrategy.RollingUpdate.ByLabel != nil {
		activePodInfos := filterOutPlaceHolderUpdateInfos(filteredPodInfos)
		return decidePodToUpdateByLabel(cls, activePodInfos)
	}
	return decidePodToUpdateByPartition(cls, filteredPodInfos)
}

func decidePodToUpdateByLabel(_ *appsv1alpha1.CollaSet, podInfos []*PodUpdateInfo) (podToUpdate []*PodUpdateInfo) {
	for i := range podInfos {
		if _, exist := podInfos[i].Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey]; exist {
			podToUpdate = append(podToUpdate, podInfos[i])
			continue
		}

		if podInfos[i].PodDecorationChanged {
			if podInfos[i].isInReplace {
				continue
			}
			// separate pd and collaset update progress
			podInfos[i].IsUpdatedRevision = true
			podInfos[i].UpdateRevision = podInfos[i].CurrentRevision
			podToUpdate = append(podToUpdate, podInfos[i])
		}
	}
	return podToUpdate
}

func decidePodToUpdateByPartition(
	cls *appsv1alpha1.CollaSet,
	filteredPodInfos []*PodUpdateInfo) []*PodUpdateInfo {

	replicas := ptr.Deref(cls.Spec.Replicas, 0)
	partition := int32(0)
	if cls.Spec.UpdateStrategy.RollingUpdate != nil && cls.Spec.UpdateStrategy.RollingUpdate.ByPartition != nil {
		partition = ptr.Deref(cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition, 0)
	}

	//filteredPodInfos := getTargetsUpdatePods(podInfos)
	// update all or not update any replicas
	if partition == 0 {
		return filteredPodInfos
	}
	if partition >= replicas {
		return nil
	}

	// partial update replicas
	ordered := orderByDefault(filteredPodInfos)
	sort.Sort(ordered)
	podToUpdate := ordered[:replicas-partition]
	for i := replicas - partition; i < replicas; i++ {
		if ordered[i].PodDecorationChanged {
			// separate pd and collaset update progress
			filteredPodInfos[i].IsUpdatedRevision = true
			ordered[i].UpdateRevision = ordered[i].CurrentRevision
			podToUpdate = append(podToUpdate, ordered[i])
		}
	}
	return podToUpdate
}

// when sort pods to choose update, only sort (1) replace origin pods, (2) non-exclude pods
func getTargetsUpdatePods(podInfos []*PodUpdateInfo) (filteredPodInfos []*PodUpdateInfo) {
	for _, podInfo := range podInfos {
		if podInfo.replacePairOriginPodName != "" {
			continue
		}

		if podInfo.PlaceHolder {
			_, isReplaceNewPod := podInfo.ContextDetail.Data[ReplaceOriginPodIDContextDataKey]
			if isReplaceNewPod {
				continue
			}
		}

		filteredPodInfos = append(filteredPodInfos, podInfo)
	}
	return filteredPodInfos
}

type orderByDefault []*PodUpdateInfo

func (o orderByDefault) Len() int {
	return len(o)
}

func (o orderByDefault) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o orderByDefault) Less(i, j int) bool {
	l, r := o[i], o[j]
	if l.IsUpdatedRevision != r.IsUpdatedRevision {
		return l.IsUpdatedRevision
	}

	if l.isDuringUpdateOps != r.isDuringUpdateOps {
		return l.isDuringUpdateOps
	}

	if l.isInReplaceUpdate != r.isInReplaceUpdate {
		return l.isInReplaceUpdate
	}

	if l.PlaceHolder != r.PlaceHolder {
		return r.PlaceHolder
	}

	if l.PlaceHolder && r.PlaceHolder {
		return true
	}

	if controllerutils.BeforeReady(l.Pod) == controllerutils.BeforeReady(r.Pod) &&
		l.PodDecorationChanged != r.PodDecorationChanged {
		return l.PodDecorationChanged
	}

	if controllerutils.IsPodServiceAvailable(l.Pod) != controllerutils.IsPodServiceAvailable(r.Pod) {
		return controllerutils.IsPodServiceAvailable(r.Pod)
	}

	return utils.ComparePod(l.Pod, r.Pod)
}

type PodUpdater interface {
	Setup(client.Client, *appsv1alpha1.CollaSet, podcontrol.Interface, record.EventRecorder)
	FulfillPodUpdatedInfo(ctx context.Context, revision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) error
	BeginUpdatePod(ctx context.Context, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (bool, error)
	FilterAllowOpsPods(ctx context.Context, podToUpdate []*PodUpdateInfo, ownedIDs map[int]*appsv1alpha1.ContextDetail, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (*time.Duration, error)
	UpgradePod(ctx context.Context, podInfo *PodUpdateInfo) error
	GetPodUpdateFinishStatus(ctx context.Context, podUpdateInfo *PodUpdateInfo) (bool, string, error)
	FinishUpdatePod(ctx context.Context, podInfo *PodUpdateInfo, finishByCancelUpdate bool) error
}

type GenericPodUpdater struct {
	*appsv1alpha1.CollaSet
	PodControl podcontrol.Interface
	Recorder   record.EventRecorder
	client.Client
}

func (u *GenericPodUpdater) Setup(client client.Client, cls *appsv1alpha1.CollaSet, podControl podcontrol.Interface, recorder record.EventRecorder) {
	u.Client = client
	u.CollaSet = cls
	u.PodControl = podControl
	u.Recorder = recorder
}

func (u *GenericPodUpdater) BeginUpdatePod(_ context.Context, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (bool, error) {
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(int, error) error {
		podInfo := <-podCh
		u.Recorder.Eventf(podInfo.Pod, corev1.EventTypeNormal, "PodUpdateLifecycle", "try to begin PodOpsLifecycle for updating Pod of CollaSet")

		if updated, err := podopslifecycle.BeginWithCleaningOld(u.Client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod, func(obj client.Object) (bool, error) {
			if !podInfo.OnlyMetadataChanged && !podInfo.InPlaceUpdateSupport {
				return podopslifecycle.WhenBeginDelete(obj)
			}
			return false, nil
		}); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
		} else if updated {
			// add an expectation for this pod update, before next reconciling
			if err := collasetutils.ActiveExpectations.ExpectUpdate(u.CollaSet, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
				return err
			}
		}

		return nil
	})

	updating := succCount > 0
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, err, "UpdateFailed", err.Error())
		return updating, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}
	return updating, nil
}

func (u *GenericPodUpdater) FilterAllowOpsPods(_ context.Context, candidates []*PodUpdateInfo, ownedIDs map[int]*appsv1alpha1.ContextDetail, _ *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (*time.Duration, error) {
	var recordedRequeueAfter *time.Duration
	needUpdateContext := false
	for i := range candidates {
		podInfo := candidates[i]

		if !podInfo.PlaceHolder {
			if !podInfo.isAllowUpdateOps {
				continue
			}
			if podInfo.requeueForOperationDelay != nil {
				u.Recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "delay Pod update for %f seconds", podInfo.requeueForOperationDelay.Seconds())
				if recordedRequeueAfter == nil || *podInfo.requeueForOperationDelay < *recordedRequeueAfter {
					recordedRequeueAfter = podInfo.requeueForOperationDelay
				}
				continue
			}
		}

		podInfo.isAllowUpdateOps = true

		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		if _, exist := ownedIDs[podInfo.ID]; !exist {
			u.Recorder.Eventf(u.CollaSet, corev1.EventTypeWarning, "PodBeforeUpdate", "pod %s/%s is not allowed to update because cannot find context id %s in resourceContext", podInfo.Namespace, podInfo.Name, podInfo.Labels[appsv1alpha1.PodInstanceIDLabelKey])
			continue
		}

		if !ownedIDs[podInfo.ID].Contains(podcontext.RevisionContextDataKey, podInfo.UpdateRevision.Name) {
			needUpdateContext = true
			ownedIDs[podInfo.ID].Put(podcontext.RevisionContextDataKey, podInfo.UpdateRevision.Name)
		}

		// mark podContext "PodRecreateUpgrade" if upgrade by recreate
		isRecreateUpdatePolicy := u.CollaSet.Spec.UpdateStrategy.PodUpdatePolicy == appsv1alpha1.CollaSetRecreatePodUpdateStrategyType
		if (!podInfo.OnlyMetadataChanged && !podInfo.InPlaceUpdateSupport) || isRecreateUpdatePolicy {
			ownedIDs[podInfo.ID].Put(podcontext.RecreateUpdateContextDataKey, "true")
		}

		if podInfo.PodDecorationChanged {
			decorationStr := anno.GetDecorationInfoString(podInfo.UpdatedPodDecorations)
			if val, ok := ownedIDs[podInfo.ID].Get(podcontext.PodDecorationRevisionKey); !ok || val != decorationStr {
				needUpdateContext = true
				ownedIDs[podInfo.ID].Put(podcontext.PodDecorationRevisionKey, decorationStr)
			}
		}

		if podInfo.PlaceHolder {
			continue
		}

		// if Pod has not been updated, update it.
		podCh <- candidates[i]
	}
	// mark Pod to use updated revision before updating it.
	if needUpdateContext {
		u.Recorder.Eventf(u.CollaSet, corev1.EventTypeNormal, "UpdateToPodContext", "try to update ResourceContext for CollaSet")
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(u.Client, u.CollaSet, ownedIDs)
		})
		return recordedRequeueAfter, err
	}
	return recordedRequeueAfter, nil
}

func (u *GenericPodUpdater) FinishUpdatePod(_ context.Context, podInfo *PodUpdateInfo, finishByCancelUpdate bool) error {
	if finishByCancelUpdate {
		// cancel update lifecycle
		return ojutils.CancelOpsLifecycle(u.Client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod)
	}

	// pod is ops finished, finish the lifecycle gracefully
	if updated, err := podopslifecycle.Finish(u.Client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod); err != nil {
		return fmt.Errorf("failed to finish PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
	} else if updated {
		// add an expectation for this pod update, before next reconciling
		if err := collasetutils.ActiveExpectations.ExpectUpdate(u.CollaSet, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
			return err
		}
		u.Recorder.Eventf(podInfo.Pod,
			corev1.EventTypeNormal,
			"UpdateReady", "pod %s/%s update finished", podInfo.Namespace, podInfo.Name)
	}
	return nil
}

// Support users to define inPlaceOnlyPodUpdater and register through RegisterInPlaceOnlyUpdater
var inPlaceOnlyPodUpdater PodUpdater

func RegisterInPlaceOnlyUpdater(podUpdater PodUpdater) {
	inPlaceOnlyPodUpdater = podUpdater
}

func newPodUpdater(client client.Client, cls *appsv1alpha1.CollaSet, podControl podcontrol.Interface, recorder record.EventRecorder) PodUpdater {
	var podUpdater PodUpdater
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType:
		podUpdater = &recreatePodUpdater{}
	case appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType:
		if inPlaceOnlyPodUpdater != nil {
			podUpdater = inPlaceOnlyPodUpdater
		} else {
			// In case of using native K8s, Pod is only allowed to update with container image, so InPlaceOnly policy is
			// implemented with InPlaceIfPossible policy as default for compatibility.
			podUpdater = &inPlaceIfPossibleUpdater{}
		}
	case appsv1alpha1.CollaSetReplacePodUpdateStrategyType:
		podUpdater = &replaceUpdatePodUpdater{}
	default:
		podUpdater = &inPlaceIfPossibleUpdater{}
	}
	podUpdater.Setup(client, cls, podControl, recorder)
	return podUpdater
}

type PodStatus struct {
	ContainerStates map[string]*ContainerStatus `json:"containerStates,omitempty"`
}

type ContainerStatus struct {
	LatestImage string `json:"latestImage,omitempty"`
	LastImageID string `json:"lastImageID,omitempty"`
}

type inPlaceIfPossibleUpdater struct {
	GenericPodUpdater
}

func (u *inPlaceIfPossibleUpdater) FulfillPodUpdatedInfo(_ context.Context, _ *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) error {
	// 1. build pod from current and updated revision
	ownerRef := metav1.NewControllerRef(u.CollaSet, appsv1alpha1.SchemeGroupVersion.WithKind("CollaSet"))
	// TODO: use cache
	currentPod, err := collasetutils.NewPodFrom(u.CollaSet, ownerRef, podUpdateInfo.CurrentRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.CurrentPodDecorations)
	})
	if err != nil {
		return fmt.Errorf("fail to build Pod from current revision %s: %v", podUpdateInfo.CurrentRevision.Name, err)
	}

	// TODO: use cache
	podUpdateInfo.UpdatedPod, err = collasetutils.NewPodFrom(u.CollaSet, ownerRef, podUpdateInfo.UpdateRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.UpdatedPodDecorations)
	})
	if err != nil {
		return fmt.Errorf("fail to build Pod from updated revision %s: %v", podUpdateInfo.UpdateRevision.Name, err)
	}

	if podUpdateInfo.PvcTmpHashChanged {
		podUpdateInfo.InPlaceUpdateSupport, podUpdateInfo.OnlyMetadataChanged = false, false
		return nil
	}

	// 2. compare current and updated pods. Only pod image and metadata are supported to update in-place
	// TODO: use cache
	var imageChangedContainers sets.String
	podUpdateInfo.InPlaceUpdateSupport, podUpdateInfo.OnlyMetadataChanged, imageChangedContainers = u.diffPod(currentPod, podUpdateInfo.UpdatedPod)
	// 3. if pod has changes more than metadata and image
	if !podUpdateInfo.InPlaceUpdateSupport {
		return nil
	}

	podUpdateInfo.UpdatedPod, err = utils.PatchToPod(currentPod, podUpdateInfo.UpdatedPod, podUpdateInfo.Pod)
	if err != nil {
		return err
	}

	if podUpdateInfo.OnlyMetadataChanged {
		if podUpdateInfo.UpdatedPod.Annotations != nil {
			delete(podUpdateInfo.UpdatedPod.Annotations, appsv1alpha1.LastPodStatusAnnotationKey)
		}
	} else {
		containerCurrentStatusMapping := map[string]*corev1.ContainerStatus{}
		for i := range podUpdateInfo.Status.ContainerStatuses {
			status := podUpdateInfo.Status.ContainerStatuses[i]
			// only store and compare imageID of changed containers
			if imageChangedContainers != nil && imageChangedContainers.Has(status.Name) {
				containerCurrentStatusMapping[status.Name] = &status
			}
		}

		podStatus := &PodStatus{ContainerStates: map[string]*ContainerStatus{}}
		for _, container := range podUpdateInfo.UpdatedPod.Spec.Containers {
			podStatus.ContainerStates[container.Name] = &ContainerStatus{
				// store image of each container in updated Pod
				LatestImage: container.Image,
			}

			containerCurrentStatus, exist := containerCurrentStatusMapping[container.Name]
			if !exist {
				continue
			}

			// store image ID of each container in current Pod
			podStatus.ContainerStates[container.Name].LastImageID = containerCurrentStatus.ImageID
		}

		podStatusStr, err := json.Marshal(podStatus)
		if err != nil {
			return err
		}

		if podUpdateInfo.UpdatedPod.Annotations == nil {
			podUpdateInfo.UpdatedPod.Annotations = map[string]string{}
		}
		podUpdateInfo.UpdatedPod.Annotations[appsv1alpha1.LastPodStatusAnnotationKey] = string(podStatusStr)
	}
	return nil
}

func (u *inPlaceIfPossibleUpdater) UpgradePod(_ context.Context, podInfo *PodUpdateInfo) error {
	if podInfo.OnlyMetadataChanged || podInfo.InPlaceUpdateSupport {
		// if pod template changes only include metadata or support in-place update, just apply these changes to pod directly
		if err := u.PodControl.UpdatePod(podInfo.UpdatedPod); err != nil {
			return fmt.Errorf("fail to update Pod %s/%s when updating by in-place: %s", podInfo.Namespace, podInfo.Name, err)
		} else {
			podInfo.Pod = podInfo.UpdatedPod
			u.Recorder.Eventf(podInfo.Pod,
				corev1.EventTypeNormal,
				"UpdatePod",
				"succeed to update Pod %s/%s to from revision %s to revision %s by in-place",
				podInfo.Namespace, podInfo.Name,
				podInfo.CurrentRevision.Name,
				podInfo.UpdateRevision.Name)
			if err := collasetutils.ActiveExpectations.ExpectUpdate(u.CollaSet, expectations.Pod, podInfo.Name, podInfo.UpdatedPod.ResourceVersion); err != nil {
				return err
			}
		}
	} else {
		// if pod has changes not in-place supported, recreate it
		return RecreatePod(u.CollaSet, podInfo, u.PodControl, u.Recorder)
	}
	return nil
}

func RecreatePod(collaSet *appsv1alpha1.CollaSet, podInfo *PodUpdateInfo, podControl podcontrol.Interface, recorder record.EventRecorder) error {
	if err := podControl.DeletePod(podInfo.Pod); err != nil {
		return fmt.Errorf("fail to delete Pod %s/%s when updating by recreate: %s", podInfo.Namespace, podInfo.Name, err)
	}
	recorder.Eventf(podInfo.Pod,
		corev1.EventTypeNormal,
		"UpdatePod",
		"succeed to update Pod %s/%s to from revision %s to revision %s by recreate",
		podInfo.Namespace,
		podInfo.Name,
		podInfo.CurrentRevision.Name,
		podInfo.UpdateRevision.Name)
	if err := collasetutils.ActiveExpectations.ExpectDelete(collaSet, expectations.Pod, podInfo.Name); err != nil {
		return err
	}

	return nil
}

func (u *inPlaceIfPossibleUpdater) diffPod(currentPod, updatedPod *corev1.Pod) (inPlaceSetUpdateSupport bool, onlyMetadataChanged bool, imageChangedContainers sets.String) {
	if len(currentPod.Spec.Containers) != len(updatedPod.Spec.Containers) {
		return false, false, nil
	}

	currentPod = currentPod.DeepCopy()
	// sync metadata
	currentPod.ObjectMeta = updatedPod.ObjectMeta

	// sync image
	imageChanged := false
	imageChangedContainers = sets.String{}
	for i := range currentPod.Spec.Containers {
		if currentPod.Spec.Containers[i].Image != updatedPod.Spec.Containers[i].Image {
			imageChanged = true
			imageChangedContainers.Insert(currentPod.Spec.Containers[i].Name)
			currentPod.Spec.Containers[i].Image = updatedPod.Spec.Containers[i].Image
		}
	}

	if !equality.Semantic.DeepEqual(currentPod, updatedPod) {
		return false, false, nil
	}

	if !imageChanged {
		return true, true, nil
	}

	return true, false, imageChangedContainers
}

func (u *inPlaceIfPossibleUpdater) GetPodUpdateFinishStatus(_ context.Context, podUpdateInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	if podUpdateInfo.PodDecorationChanged {
		return false, "add on not updated", nil
	}

	if podUpdateInfo.Status.ContainerStatuses == nil {
		return false, "no container status", nil
	}

	if podUpdateInfo.Spec.Containers == nil {
		return false, "no container spec", nil
	}

	if len(podUpdateInfo.Spec.Containers) != len(podUpdateInfo.Status.ContainerStatuses) {
		return false, "container status number does not match", nil
	}

	if podUpdateInfo.Annotations == nil {
		return false, "no annotations for last container status", nil
	}

	podLastState := &PodStatus{}
	if lastStateJson, exist := podUpdateInfo.Annotations[appsv1alpha1.LastPodStatusAnnotationKey]; !exist {
		return false, "no pod last state annotation", nil
	} else if err := json.Unmarshal([]byte(lastStateJson), podLastState); err != nil {
		msg := fmt.Sprintf("malformat pod last state annotation [%s]: %s", lastStateJson, err)
		return false, msg, fmt.Errorf("malformat pod last state annotation [%s]: %s", lastStateJson, err)
	}

	if podLastState.ContainerStates == nil {
		return true, "empty last container state recorded", nil
	}

	imageMapping := map[string]string{}
	for _, containerSpec := range podUpdateInfo.Spec.Containers {
		imageMapping[containerSpec.Name] = containerSpec.Image
	}

	imageIdMapping := map[string]string{}
	for _, containerStatus := range podUpdateInfo.Status.ContainerStatuses {
		imageIdMapping[containerStatus.Name] = containerStatus.ImageID
	}

	for containerName, lastContainerState := range podLastState.ContainerStates {
		latestImage := lastContainerState.LatestImage
		lastImageId := lastContainerState.LastImageID

		if currentImage, exist := imageMapping[containerName]; !exist {
			// If no this container image recorded, ignore this container.
			continue
		} else if currentImage != latestImage {
			// If container image in pod spec has changed, ignore this container.
			continue
		}

		if currentImageId, exist := imageIdMapping[containerName]; !exist {
			// If no this container image id recorded, ignore this container.
			continue
		} else if currentImageId == lastImageId {
			// No image id changed means the pod in-place update has not finished by kubelet.
			return false, fmt.Sprintf("container has %s not been updated: last image id %s, current image id %s", containerName, lastImageId, currentImageId), nil
		}
	}

	return true, "", nil
}

type recreatePodUpdater struct {
	GenericPodUpdater
}

func (u *recreatePodUpdater) FulfillPodUpdatedInfo(_ context.Context, _ *appsv1.ControllerRevision, _ *PodUpdateInfo) error {
	return nil
}

func (u *recreatePodUpdater) UpgradePod(_ context.Context, podInfo *PodUpdateInfo) error {
	return RecreatePod(u.CollaSet, podInfo, u.PodControl, u.Recorder)
}

func (u *recreatePodUpdater) GetPodUpdateFinishStatus(_ context.Context, podInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	// Recreate policy always treat Pod as update not finished
	return podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged, "", nil
}

type replaceUpdatePodUpdater struct {
	collaSet   *appsv1alpha1.CollaSet
	podControl podcontrol.Interface
	recorder   record.EventRecorder
	client.Client
}

func (u *replaceUpdatePodUpdater) Setup(client client.Client, cls *appsv1alpha1.CollaSet, podControl podcontrol.Interface, recorder record.EventRecorder) {
	u.Client = client
	u.collaSet = cls
	u.podControl = podControl
	u.recorder = recorder
}

func (u *replaceUpdatePodUpdater) BeginUpdatePod(ctx context.Context, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (bool, error) {
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(int, error) error {
		podInfo := <-podCh
		if podInfo.replacePairNewPodInfo != nil {
			replacePairNewPod := podInfo.replacePairNewPodInfo.Pod
			newPodRevision, exist := replacePairNewPod.Labels[appsv1.ControllerRevisionHashLabelKey]
			if exist && newPodRevision == podInfo.UpdateRevision.Name {
				return nil
			}
			if _, exist := replacePairNewPod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; exist {
				return nil
			}

			u.recorder.Eventf(podInfo.Pod,
				corev1.EventTypeNormal,
				"ReplaceUpdatePod",
				"label to-delete on new pair pod %s/%s because it is not updated revision, current revision: %s, updated revision: %s",
				replacePairNewPod.Namespace,
				replacePairNewPod.Name,
				newPodRevision,
				resources.UpdatedRevision.Name)
			patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%d"}}}`, appsv1alpha1.PodDeletionIndicationLabelKey, time.Now().UnixNano())))
			if patchErr := u.Patch(ctx, podInfo.replacePairNewPodInfo.Pod, patch); patchErr != nil {
				err := fmt.Errorf("failed to delete replace pair new pod %s/%s %s",
					podInfo.replacePairNewPodInfo.Namespace, podInfo.replacePairNewPodInfo.Name, patchErr)
				return err
			}
		}
		return nil
	})

	return succCount > 0, err
}

func (u *replaceUpdatePodUpdater) FilterAllowOpsPods(_ context.Context, candidates []*PodUpdateInfo, _ map[int]*appsv1alpha1.ContextDetail, _ *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (requeueAfter *time.Duration, err error) {
	activePodToUpdate := filterOutPlaceHolderUpdateInfos(candidates)
	for i, podInfo := range activePodToUpdate {
		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		podCh <- activePodToUpdate[i]
	}
	return nil, err
}

func (u *replaceUpdatePodUpdater) FulfillPodUpdatedInfo(_ context.Context, _ *appsv1.ControllerRevision, _ *PodUpdateInfo) (err error) {
	return
}

func (u *replaceUpdatePodUpdater) UpgradePod(ctx context.Context, podInfo *PodUpdateInfo) error {
	return updateReplaceOriginPod(ctx, u.Client, u.recorder, podInfo, podInfo.replacePairNewPodInfo)
}

func (u *replaceUpdatePodUpdater) GetPodUpdateFinishStatus(_ context.Context, podUpdateInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	replaceNewPodInfo := podUpdateInfo.replacePairNewPodInfo
	if replaceNewPodInfo == nil {
		return
	}

	return isPodUpdatedServiceAvailable(replaceNewPodInfo)
}

func (u *replaceUpdatePodUpdater) FinishUpdatePod(_ context.Context, podInfo *PodUpdateInfo, finishByCancelUpdate bool) error {
	if finishByCancelUpdate {
		// cancel replace update by removing to-replace and replace-by-update label from origin pod
		if podInfo.isInReplace {
			patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":null, "%s":null}}}`, appsv1alpha1.PodReplaceIndicationLabelKey, appsv1alpha1.PodReplaceByReplaceUpdateLabelKey)))
			if err := u.podControl.PatchPod(podInfo.Pod, patch); err != nil {
				return fmt.Errorf("failed to delete replace pair origin pod %s/%s %s", podInfo.Namespace, podInfo.Name, err)
			}
		}
		return nil
	}

	replacePairNewPodInfo := podInfo.replacePairNewPodInfo
	if replacePairNewPodInfo != nil {
		if _, exist := podInfo.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; !exist {
			patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%d"}}}`, appsv1alpha1.PodDeletionIndicationLabelKey, time.Now().UnixNano())))
			if err := u.podControl.PatchPod(podInfo.Pod, patch); err != nil {
				return fmt.Errorf("failed to delete replace pair origin pod %s/%s %s", podInfo.Namespace, podInfo.replacePairNewPodInfo.Name, err)
			}
		}
	}
	return nil
}

func isPodUpdatedServiceAvailable(podInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	if podInfo.PodDecorationChanged {
		return false, "add on not updated", nil
	}

	if podInfo.Labels == nil {
		return false, "no labels on pod", nil
	}
	if podInfo.isInReplace && podInfo.replacePairNewPodInfo != nil {
		return false, "replace origin pod", nil
	}

	if _, serviceAvailable := podInfo.Labels[appsv1alpha1.PodServiceAvailableLabel]; serviceAvailable {
		return true, "", nil
	}

	return false, "pod not service available", nil
}
