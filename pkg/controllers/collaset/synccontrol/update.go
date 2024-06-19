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
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontext"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/operating/pkg/controllers/collaset/pvccontrol"
	"kusionstack.io/operating/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	commonutils "kusionstack.io/operating/pkg/utils"
)

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

	// indicates the PodOpsLifecycle is started.
	isDuringOps bool

	// for replace update
	// judge pod in replace updating
	isInReplacing bool

	// replace new created pod
	replacePairNewPodInfo *PodUpdateInfo

	// replace origin pod
	replacePairOriginPodName string
}

func attachPodUpdateInfo(ctx context.Context, cls *appsv1alpha1.CollaSet, pods []*collasetutils.PodWrapper, resource *collasetutils.RelatedResources) ([]*PodUpdateInfo, error) {
	podUpdateInfoList := make([]*PodUpdateInfo, len(pods))

	for i, pod := range pods {
		updateInfo := &PodUpdateInfo{
			PodWrapper: pod,
		}

		if pod.IsPlaceHolder {
			updateInfo.UpdateRevision = resource.UpdatedRevision
			if revision, exist := pod.ContextDetail.Data[podcontext.RevisionContextDataKey]; exist &&
				revision == resource.UpdatedRevision.Name {
				updateInfo.IsUpdatedRevision = true
			}
			podUpdateInfoList[i] = updateInfo
			continue
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

		// decide whether the PodOpsLifecycle is during ops or not
		updateInfo.isDuringOps = podopslifecycle.IsDuringOps(utils.UpdateOpsLifecycleAdapter, pod)
		updateInfo.PvcTmpHashChanged, err = pvccontrol.IsPodPvcTmpChanged(cls, pod.Pod, resource.ExistingPvcs)
		if err != nil {
			return nil, fmt.Errorf("fail to check pvc template changed, %v", err)
		}
		podUpdateInfoList[i] = updateInfo
	}

	// attach replace info
	var podUpdateInfoMap = make(map[string]*PodUpdateInfo)
	for _, podUpdateInfo := range podUpdateInfoList {
		if podUpdateInfo.IsPlaceHolder {
			continue
		}
		podUpdateInfoMap[podUpdateInfo.Name] = podUpdateInfo
	}
	replacePodMap := classifyPodReplacingMapping(pods)
	for originPodName, replacePairNewPod := range replacePodMap {
		originPodInfo := podUpdateInfoMap[originPodName]
		if replacePairNewPod != nil {
			originPodInfo.isInReplacing = true
			// replace origin pod not go through lifecycle, mark  during ops manual
			originPodInfo.isDuringOps = true
			replacePairNewPodInfo := podUpdateInfoMap[replacePairNewPod.Name]
			replacePairNewPodInfo.isInReplacing = true
			replacePairNewPodInfo.replacePairOriginPodName = originPodName
			originPodInfo.replacePairNewPodInfo = replacePairNewPodInfo
		} else {
			_, replaceIndicated := originPodInfo.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			_, replaceByReplaceUpdate := originPodInfo.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
			if replaceIndicated && replaceByReplaceUpdate {
				originPodInfo.isInReplacing = true
				originPodInfo.isDuringOps = true
			}
		}
	}

	return podUpdateInfoList, nil
}

func decidePodToUpdate(
	cls *appsv1alpha1.CollaSet,
	podInfos []*PodUpdateInfo) []*PodUpdateInfo {

	if cls.Spec.UpdateStrategy.RollingUpdate != nil && cls.Spec.UpdateStrategy.RollingUpdate.ByLabel != nil {
		return decidePodToUpdateByLabel(cls, podInfos)
	}

	return decidePodToUpdateByPartition(cls, podInfos)
}

func decidePodToUpdateByLabel(_ *appsv1alpha1.CollaSet, podInfos []*PodUpdateInfo) (podToUpdate []*PodUpdateInfo) {
	for i := range podInfos {
		if podInfos[i].IsPlaceHolder {
			continue
		}
		if _, exist := podInfos[i].Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey]; exist {
			// filter pod which is in replace update and is the new created pod
			if podInfos[i].isInReplacing && podInfos[i].replacePairOriginPodName != "" {
				continue
			}
			podToUpdate = append(podToUpdate, podInfos[i])
			continue
		}

		// already in replace update.
		if podInfos[i].isInReplacing && podInfos[i].replacePairNewPodInfo != nil {
			podToUpdate = append(podToUpdate, podInfos[i])
			continue
		}
		if podInfos[i].PodDecorationChanged {
			if podInfos[i].isInReplacing && podInfos[i].replacePairOriginPodName != "" {
				continue
			}
			podToUpdate = append(podToUpdate, podInfos[i])
		}
	}
	return podToUpdate
}

func decidePodToUpdateByPartition(
	cls *appsv1alpha1.CollaSet,
	podInfos []*PodUpdateInfo) (podToUpdate []*PodUpdateInfo) {

	filteredPodInfos := filterReplacingNewCreatedPod(podInfos)
	if cls.Spec.UpdateStrategy.RollingUpdate == nil ||
		cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition == nil {
		return filteredPodInfos
	}
	podsNum := len(filteredPodInfos)
	ordered := orderByDefault(filteredPodInfos)
	sort.Sort(ordered)

	partition := int(*cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition)
	if partition >= podsNum {
		return podToUpdate
	}

	podToUpdate = ordered[:podsNum-partition]
	for i := podsNum - partition; i < podsNum; i++ {
		if podInfos[i].PodDecorationChanged {
			podToUpdate = append(podToUpdate, podInfos[i])
		}
	}
	return podToUpdate
}

// filter these pods in replacing and is new created pod
func filterReplacingNewCreatedPod(podInfos []*PodUpdateInfo) (filteredPodInfos []*PodUpdateInfo) {
	for _, podInfo := range podInfos {
		if podInfo.isInReplacing && podInfo.replacePairOriginPodName != "" {
			continue
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

	if l.isDuringOps != r.isDuringOps {
		return l.isDuringOps
	}

	if l.IsPlaceHolder != r.IsPlaceHolder {
		return r.IsPlaceHolder
	}

	if l.IsPlaceHolder && r.IsPlaceHolder {
		return true
	}

	if controllerutils.BeforeReady(l.Pod) == controllerutils.BeforeReady(r.Pod) &&
		l.PodDecorationChanged != r.PodDecorationChanged {
		return l.PodDecorationChanged
	}

	return utils.ComparePod(l.Pod, r.Pod)
}

type PodUpdater interface {
	FulfillPodUpdatedInfo(revision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) error
	BeginUpdatePod(resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (bool, error)
	FilterAllowOpsPods(podToUpdate []*PodUpdateInfo, ownedIDs map[int]*appsv1alpha1.ContextDetail, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (*time.Duration, error)
	UpgradePod(podInfo *PodUpdateInfo) error
	GetPodUpdateFinishStatus(podUpdateInfo *PodUpdateInfo) (bool, string, error)
	FinishUpdatePod(podInfo *PodUpdateInfo) error
}

type GenericPodUpdater struct {
	collaSet   *appsv1alpha1.CollaSet
	ctx        context.Context
	podControl podcontrol.Interface
	recorder   record.EventRecorder
	client.Client
}

func (u *GenericPodUpdater) BeginUpdatePod(resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (bool, error) {
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(int, error) error {
		podInfo := <-podCh
		if podInfo.IsPlaceHolder {
			return nil
		}
		u.recorder.Eventf(podInfo.Pod, corev1.EventTypeNormal, "PodUpdateLifecycle", "try to begin PodOpsLifecycle for updating Pod of CollaSet")
		if updated, err := podopslifecycle.Begin(u.Client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod, func(obj client.Object) (bool, error) {
			if !podInfo.OnlyMetadataChanged && !podInfo.InPlaceUpdateSupport {
				return podopslifecycle.WhenBeginDelete(obj)
			}
			return false, nil
		}); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
		} else if updated {
			// add an expectation for this pod update, before next reconciling
			if err := collasetutils.ActiveExpectations.ExpectUpdate(u.collaSet, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
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

func (u *GenericPodUpdater) FilterAllowOpsPods(podToUpdate []*PodUpdateInfo, ownedIDs map[int]*appsv1alpha1.ContextDetail, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (*time.Duration, error) {
	var recordedRequeueAfter *time.Duration
	needUpdateContext := false
	for i := range podToUpdate {
		podInfo := podToUpdate[i]
		if !podInfo.IsPlaceHolder {
			requeueAfter, allowed := podopslifecycle.AllowOps(collasetutils.UpdateOpsLifecycleAdapter, realValue(u.collaSet.Spec.UpdateStrategy.OperationDelaySeconds), podInfo.Pod)
			if !allowed {
				u.recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "Pod %s is not allowed to update", commonutils.ObjectKeyString(podInfo.Pod))
				continue
			}
			if requeueAfter != nil {
				u.recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "delay Pod update for %d seconds", requeueAfter.Seconds())
				if recordedRequeueAfter == nil || *requeueAfter < *recordedRequeueAfter {
					recordedRequeueAfter = requeueAfter
				}
				continue
			}
		}

		if !ownedIDs[podInfo.ID].Contains(podcontext.RevisionContextDataKey, resources.UpdatedRevision.Name) {
			needUpdateContext = true
			ownedIDs[podInfo.ID].Put(podcontext.RevisionContextDataKey, resources.UpdatedRevision.Name)
		}

		// mark podContext "PodRecreateUpgrade" if upgrade by recreate
		if !podInfo.OnlyMetadataChanged && !podInfo.InPlaceUpdateSupport {
			ownedIDs[podInfo.ID].Put(podcontext.RecreateUpdateContextDataKey, "true")
		}

		if podInfo.PodDecorationChanged {
			decorationStr := utilspoddecoration.GetDecorationInfoString(podInfo.UpdatedPodDecorations)
			if val, ok := ownedIDs[podInfo.ID].Get(podcontext.PodDecorationRevisionKey); !ok || val != decorationStr {
				needUpdateContext = true
				ownedIDs[podInfo.ID].Put(podcontext.PodDecorationRevisionKey, decorationStr)
			}
		}

		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		if podInfo.IsPlaceHolder {
			continue
		}

		// if Pod has not been updated, update it.
		podCh <- podToUpdate[i]
	}
	// mark Pod to use updated revision before updating it.
	if needUpdateContext {
		u.recorder.Eventf(u.collaSet, corev1.EventTypeNormal, "UpdateToPodContext", "try to update ResourceContext for CollaSet")
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(u.Client, u.collaSet, ownedIDs)
		})
		return recordedRequeueAfter, err
	}
	return recordedRequeueAfter, nil
}

func (u *GenericPodUpdater) FinishUpdatePod(podInfo *PodUpdateInfo) error {
	if updated, err := podopslifecycle.Finish(u.Client, collasetutils.UpdateOpsLifecycleAdapter, podInfo.Pod); err != nil {
		return fmt.Errorf("failed to finish PodOpsLifecycle for updating Pod %s/%s: %s", podInfo.Namespace, podInfo.Name, err)
	} else if updated {
		// add an expectation for this pod update, before next reconciling
		if err := collasetutils.ActiveExpectations.ExpectUpdate(u.collaSet, expectations.Pod, podInfo.Name, podInfo.ResourceVersion); err != nil {
			return err
		}
		u.recorder.Eventf(podInfo.Pod,
			corev1.EventTypeNormal,
			"UpdateReady", "pod %s/%s update finished", podInfo.Namespace, podInfo.Name)
	}
	return nil
}

func newPodUpdater(ctx context.Context, client client.Client, cls *appsv1alpha1.CollaSet, podControl podcontrol.Interface, recorder record.EventRecorder) PodUpdater {
	genericPodUpdater := &GenericPodUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder}
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType:
		return &recreatePodUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder, GenericPodUpdater: *genericPodUpdater}
	case appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType:
		// In case of using native K8s, Pod is only allowed to update with container image, so InPlaceOnly policy is
		// implemented with InPlaceIfPossible policy as default for compatibility.
		return &inPlaceIfPossibleUpdater{collaSet: cls, ctx: ctx, Client: client}
	case appsv1alpha1.CollaSetReplacePodUpdateStrategyType:
		return &replaceUpdatePodUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder}
	default:
		return &inPlaceIfPossibleUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder, GenericPodUpdater: *genericPodUpdater}
	}
}

type PodStatus struct {
	ContainerStates map[string]*ContainerStatus `json:"containerStates,omitempty"`
}

type ContainerStatus struct {
	LatestImage string `json:"latestImage,omitempty"`
	LastImageID string `json:"lastImageID,omitempty"`
}

type inPlaceIfPossibleUpdater struct {
	collaSet   *appsv1alpha1.CollaSet
	ctx        context.Context
	podControl podcontrol.Interface
	recorder   record.EventRecorder
	GenericPodUpdater
	client.Client
}

func (u *inPlaceIfPossibleUpdater) FulfillPodUpdatedInfo(
	updatedRevision *appsv1.ControllerRevision,
	podUpdateInfo *PodUpdateInfo) error {
	if podUpdateInfo.IsPlaceHolder {
		return nil
	}
	// 1. build pod from current and updated revision
	ownerRef := metav1.NewControllerRef(u.collaSet, appsv1alpha1.GroupVersion.WithKind("CollaSet"))
	// TODO: use cache
	currentPod, err := collasetutils.NewPodFrom(u.collaSet, ownerRef, podUpdateInfo.CurrentRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.CurrentPodDecorations)
	})
	if err != nil {
		return fmt.Errorf("fail to build Pod from current revision %s: %v", podUpdateInfo.CurrentRevision.Name, err)
	}

	// TODO: use cache
	podUpdateInfo.UpdatedPod, err = collasetutils.NewPodFrom(u.collaSet, ownerRef, updatedRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.UpdatedPodDecorations)
	})
	if err != nil {
		return fmt.Errorf("fail to build Pod from updated revision %s: %v", updatedRevision.Name, err)
	}

	if podUpdateInfo.PvcTmpHashChanged {
		podUpdateInfo.InPlaceUpdateSupport, podUpdateInfo.OnlyMetadataChanged = false, false
		return nil
	}

	// 2. compare current and updated pods. Only pod image and metadata are supported to update in-place
	// TODO: use cache
	podUpdateInfo.InPlaceUpdateSupport, podUpdateInfo.OnlyMetadataChanged = u.diffPod(currentPod, podUpdateInfo.UpdatedPod)
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
			containerCurrentStatusMapping[status.Name] = &status
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

func (u *inPlaceIfPossibleUpdater) UpgradePod(podInfo *PodUpdateInfo) error {
	if podInfo.OnlyMetadataChanged || podInfo.InPlaceUpdateSupport {
		// if pod template changes only include metadata or support in-place update, just apply these changes to pod directly
		if err := u.podControl.UpdatePod(podInfo.UpdatedPod); err != nil {
			return fmt.Errorf("fail to update Pod %s/%s when updating by in-place: %s", podInfo.Namespace, podInfo.Name, err)
		} else {
			podInfo.Pod = podInfo.UpdatedPod
			u.recorder.Eventf(podInfo.Pod,
				corev1.EventTypeNormal,
				"UpdatePod",
				"succeed to update Pod %s/%s to from revision %s to revision %s by in-place",
				podInfo.Namespace, podInfo.Name,
				podInfo.CurrentRevision.Name,
				podInfo.UpdateRevision.Name)
			if err := collasetutils.ActiveExpectations.ExpectUpdate(u.collaSet, expectations.Pod, podInfo.Name, podInfo.UpdatedPod.ResourceVersion); err != nil {
				return err
			}
		}
	} else {
		// if pod has changes not in-place supported, recreate it
		return recreatePod(u.collaSet, podInfo, u.podControl, u.recorder)
	}
	return nil
}

func recreatePod(collaSet *appsv1alpha1.CollaSet, podInfo *PodUpdateInfo, podControl podcontrol.Interface, recorder record.EventRecorder) error {
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

func (u *inPlaceIfPossibleUpdater) diffPod(currentPod, updatedPod *corev1.Pod) (inPlaceSetUpdateSupport bool, onlyMetadataChanged bool) {
	if len(currentPod.Spec.Containers) != len(updatedPod.Spec.Containers) {
		return false, false
	}

	currentPod = currentPod.DeepCopy()
	// sync metadata
	currentPod.ObjectMeta = updatedPod.ObjectMeta

	// sync image
	imageChanged := false
	for i := range currentPod.Spec.Containers {
		if currentPod.Spec.Containers[i].Image != updatedPod.Spec.Containers[i].Image {
			imageChanged = true
			currentPod.Spec.Containers[i].Image = updatedPod.Spec.Containers[i].Image
		}
	}

	if !equality.Semantic.DeepEqual(currentPod, updatedPod) {
		return false, false
	}

	if !imageChanged {
		return true, true
	}

	return true, false
}

func (u *inPlaceIfPossibleUpdater) GetPodUpdateFinishStatus(podUpdateInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	if !podUpdateInfo.IsUpdatedRevision || podUpdateInfo.PodDecorationChanged {
		return false, "not updated revision", nil
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
		return true, "no annotations for last container status", nil
	}

	podLastState := &PodStatus{}
	if lastStateJson, exist := podUpdateInfo.Annotations[appsv1alpha1.LastPodStatusAnnotationKey]; !exist {
		return true, "no pod last state annotation", nil
	} else if err := json.Unmarshal([]byte(lastStateJson), podLastState); err != nil {
		msg := fmt.Sprintf("malformat pod last state annotation [%s]: %s", lastStateJson, err)
		return false, msg, fmt.Errorf(msg)
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

	for containerName, lastContaienrState := range podLastState.ContainerStates {
		latestImage := lastContaienrState.LatestImage
		lastImageId := lastContaienrState.LastImageID

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

// TODO
type inPlaceOnlyPodUpdater struct {
}

func (u *inPlaceOnlyPodUpdater) BeginUpdate(_ *collasetutils.RelatedResources, _ chan *PodUpdateInfo) (updating bool, err error) {
	return
}

func (u *inPlaceOnlyPodUpdater) FilterAllowOpsPodsAndUpdatePodContext(_ []*PodUpdateInfo, _ map[int]*appsv1alpha1.ContextDetail, _ *collasetutils.RelatedResources, _ chan *PodUpdateInfo) (requeueAfter *time.Duration, err error) {
	return
}

func (u *inPlaceOnlyPodUpdater) FulfillPodUpdatedInfo(_ *appsv1.ControllerRevision, _ *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {
	return
}

func (u *inPlaceOnlyPodUpdater) UpgradePod(_ *appsv1alpha1.CollaSet, _ *PodUpdateInfo) (err error) {
	return
}

func (u *inPlaceOnlyPodUpdater) GetPodUpdateFinishStatus(_ *PodUpdateInfo) (finished bool, msg string, err error) {
	return
}

type recreatePodUpdater struct {
	collaSet   *appsv1alpha1.CollaSet
	ctx        context.Context
	podControl podcontrol.Interface
	recorder   record.EventRecorder
	GenericPodUpdater
	client.Client
}

func (u *recreatePodUpdater) FulfillPodUpdatedInfo(_ *appsv1.ControllerRevision, _ *PodUpdateInfo) error {
	return nil
}

func (u *recreatePodUpdater) UpgradePod(podInfo *PodUpdateInfo) error {
	return recreatePod(u.collaSet, podInfo, u.podControl, u.recorder)
}

func (u *recreatePodUpdater) GetPodUpdateFinishStatus(podInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	// Recreate policy alway treat Pod as update finished
	return podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged, "", nil
}

type replaceUpdatePodUpdater struct {
	collaSet   *appsv1alpha1.CollaSet
	ctx        context.Context
	podControl podcontrol.Interface
	recorder   record.EventRecorder
	client.Client
}

func (u *replaceUpdatePodUpdater) BeginUpdatePod(resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (bool, error) {
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(int, error) error {
		podInfo := <-podCh
		if podInfo.IsPlaceHolder {
			return nil
		}
		if podInfo.replacePairNewPodInfo != nil {
			replacePairNewPod := podInfo.replacePairNewPodInfo.Pod
			newPodRevision, exist := replacePairNewPod.Labels[appsv1.ControllerRevisionHashLabelKey]
			if exist && newPodRevision == resources.UpdatedRevision.Name {
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
			if patchErr := u.Patch(u.ctx, podInfo.replacePairNewPodInfo.Pod, patch); patchErr != nil {
				err := fmt.Errorf("failed to delete replace pair new pod %s/%s %s",
					podInfo.replacePairNewPodInfo.Namespace, podInfo.replacePairNewPodInfo.Name, patchErr)
				return err
			}
		}
		return nil
	})

	return succCount > 0, err
}

func (u *replaceUpdatePodUpdater) FilterAllowOpsPods(podToUpdate []*PodUpdateInfo, _ map[int]*appsv1alpha1.ContextDetail, _ *collasetutils.RelatedResources, podCh chan *PodUpdateInfo) (requeueAfter *time.Duration, err error) {
	for i, podInfo := range podToUpdate {
		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		if podInfo.IsPlaceHolder {
			continue
		}

		podCh <- podToUpdate[i]
	}
	return nil, err
}

func (u *replaceUpdatePodUpdater) FulfillPodUpdatedInfo(_ *appsv1.ControllerRevision, _ *PodUpdateInfo) (err error) {
	return
}

func (u *replaceUpdatePodUpdater) UpgradePod(podInfo *PodUpdateInfo) error {
	// add replace indicate label only and wait to replace when syncPods
	if _, exist := podInfo.Pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; !exist {
		// need replace pod, label pod with replace-indicate
		now := time.Now().UnixNano()
		patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%v", "%s": "%v"}}}`, appsv1alpha1.PodReplaceIndicationLabelKey, now, appsv1alpha1.PodReplaceByReplaceUpdateLabelKey, true)))
		if err := u.Patch(u.ctx, podInfo.Pod, patch); err != nil {
			return fmt.Errorf("fail to label origin pod %s/%s with replace indicate label by replaceUpdate: %s", podInfo.Namespace, podInfo.Name, err)
		}
		u.recorder.Eventf(podInfo.Pod,
			corev1.EventTypeNormal,
			"UpdatePod",
			"succeed to update Pod %s/%s by label to-replace",
			podInfo.Namespace,
			podInfo.Name,
		)
	}
	return nil
}

func (u *replaceUpdatePodUpdater) GetPodUpdateFinishStatus(podUpdateInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	replaceNewPodInfo := podUpdateInfo.replacePairNewPodInfo
	if replaceNewPodInfo == nil {
		return
	}

	return isPodUpdatedServiceAvailable(replaceNewPodInfo)
}

func (u *replaceUpdatePodUpdater) FinishUpdatePod(podInfo *PodUpdateInfo) error {
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
	if !podInfo.IsUpdatedRevision || podInfo.PodDecorationChanged {
		return false, "not updated revision", nil
	}

	if podInfo.Labels == nil {
		return false, "no labels on pod", nil
	}
	if podInfo.isInReplacing && podInfo.replacePairNewPodInfo != nil {
		return false, "replace origin pod", nil
	}

	if _, serviceAvailable := podInfo.Labels[appsv1alpha1.PodServiceAvailableLabel]; serviceAvailable {
		return true, "", nil
	}

	return false, "pod not service available", nil
}
