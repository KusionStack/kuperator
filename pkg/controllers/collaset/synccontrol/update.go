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

	// after fulfillPodUpdateInfo, set value as true. to avoid fulfillPodUpdateInfo multiple times
	analysed bool
}

func attachPodUpdateInfo(ctx context.Context, pods []*collasetutils.PodWrapper, resource *collasetutils.RelatedResources) ([]*PodUpdateInfo, error) {
	podUpdateInfoList := make([]*PodUpdateInfo, len(pods))

	for i, pod := range pods {
		updateInfo := &PodUpdateInfo{
			PodWrapper: pod,
		}
		currentPDs, err := resource.PDGetter.GetCurrentDecorationsOnPod(ctx, pod.Pod)
		if err != nil {
			return nil, err
		}
		updatedPDs, err := resource.PDGetter.GetUpdatedDecorationsByOldPod(ctx, pod.Pod)
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

		podUpdateInfoList[i] = updateInfo
	}

	// attach replace info
	var podUpdateInfoMap = make(map[string]*PodUpdateInfo)
	for _, podUpdateInfo := range podUpdateInfoList {
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

func decidePodToUpdate(cls *appsv1alpha1.CollaSet, podInfos []*PodUpdateInfo) []*PodUpdateInfo {
	if cls.Spec.UpdateStrategy.RollingUpdate != nil && cls.Spec.UpdateStrategy.RollingUpdate.ByLabel != nil {
		return decidePodToUpdateByLabel(cls, podInfos)
	}

	return decidePodToUpdateByPartition(cls, podInfos)
}

func decidePodToUpdateByLabel(_ *appsv1alpha1.CollaSet, podInfos []*PodUpdateInfo) (podToUpdate []*PodUpdateInfo) {
	for i := range podInfos {
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

func decidePodToUpdateByPartition(cls *appsv1alpha1.CollaSet, podInfos []*PodUpdateInfo) (podToUpdate []*PodUpdateInfo) {
	filteredPodInfos := filterReplacingNewCreatedPod(podInfos)
	if cls.Spec.UpdateStrategy.RollingUpdate == nil ||
		cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition == nil {
		return filteredPodInfos
	}
	ordered := orderByDefault(filteredPodInfos)
	sort.Sort(ordered)

	partition := int(*cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition)
	if partition >= len(filteredPodInfos) {
		return filteredPodInfos
	}
	podToUpdate = filteredPodInfos[:partition]
	for i := partition; i < len(filteredPodInfos); i++ {
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

	if controllerutils.BeforeReady(l.Pod) == controllerutils.BeforeReady(r.Pod) &&
		l.PodDecorationChanged != r.PodDecorationChanged {
		return l.PodDecorationChanged
	}

	return utils.ComparePod(l.Pod, r.Pod)
}

type PodUpdater interface {
	PrepareAndFilterPodUpdate(podToUpdate []*PodUpdateInfo, resources *collasetutils.RelatedResources,
		ownedIDs map[int]*appsv1alpha1.ContextDetail, podCh chan *PodUpdateInfo) (bool, *time.Duration, error)
	FulfillPodUpdatedInfo(revision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) error
	UpgradePod(podInfo *PodUpdateInfo) error
	GetPodUpdateFinishStatus(podUpdateInfo *PodUpdateInfo) (bool, string, error)
	FinishUpdatePod(podInfo *PodUpdateInfo) error
}

func newPodUpdater(ctx context.Context, client client.Client, cls *appsv1alpha1.CollaSet, podControl podcontrol.Interface, recorder record.EventRecorder) PodUpdater {
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType:
		return &recreatePodUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder}
	case appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType:
		// In case of using native K8s, Pod is only allowed to update with container image, so InPlaceOnly policy is
		// implemented with InPlaceIfPossible policy as default for compatibility.
		return &inPlaceIfPossibleUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder}
	case appsv1alpha1.CollaSetReplacePodUpdateStrategyType:
		return &replaceUpdatePodUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder}
	default:
		return &inPlaceIfPossibleUpdater{collaSet: cls, ctx: ctx, Client: client, podControl: podControl, recorder: recorder}
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
	client.Client
}

func (u *inPlaceIfPossibleUpdater) PrepareAndFilterPodUpdate(podToUpdate []*PodUpdateInfo, resources *collasetutils.RelatedResources, ownedIDs map[int]*appsv1alpha1.ContextDetail, podCh chan *PodUpdateInfo) (bool, *time.Duration, error) {
	// 1. prepare Pods to begin PodOpsLifecycle, filter updated revision pods or is already during pods
	filterPods(podToUpdate, podCh)
	// 2. begin podOpsLifecycle parallel
	var recordedRequeueAfter *time.Duration
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(int, error) error {
		podInfo := <-podCh
		// fulfill Pod update information
		if err := u.FulfillPodUpdatedInfo(resources.UpdatedRevision, podInfo); err != nil {
			return fmt.Errorf("fail to analyse pod %s/%s in-place update support: %s", podInfo.Namespace, podInfo.Name, err)
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
		return updating, nil, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}
	// 3. judge whether it need to update podContext
	recordedRequeueAfter, err = filterPodsAndUpdatePodContext(u.collaSet, podToUpdate, u.recorder, ownedIDs, resources, podCh, u.Client)
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetScale, err, "UpdateFailed",
			fmt.Sprintf("fail to update Context for updating: %s", err))
		return updating, recordedRequeueAfter, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetScale, nil, "UpdateFailed", "")
	}
	return updating, recordedRequeueAfter, nil
}

func (u *inPlaceIfPossibleUpdater) FulfillPodUpdatedInfo(
	updatedRevision *appsv1.ControllerRevision,
	podUpdateInfo *PodUpdateInfo) error {

	podUpdateInfo.analysed = true

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
	if (podInfo.OnlyMetadataChanged || podInfo.InPlaceUpdateSupport) && !podInfo.PvcTmpHashChanged {
		// 6.1 if pod template changes only include metadata or support in-place update, just apply these changes to pod directly
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
		// 6.2 if pod has changes not in-place supported, or pvc template hash changed, recreate it
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

func (u inPlaceIfPossibleUpdater) diffPod(currentPod, updatedPod *corev1.Pod) (inPlaceSetUpdateSupport bool, onlyMetadataChanged bool) {
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

func (u *inPlaceIfPossibleUpdater) FinishUpdatePod(podInfo *PodUpdateInfo) error {
	//u.recorder.Eventf().V(1).Info("try to finish update PodOpsLifecycle for Pod", "pod", commonutils.ObjectKeyString(podInfo.Pod))
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

func (u *inPlaceOnlyPodUpdater) FulfillPodUpdatedInfo(_ *appsv1.ControllerRevision, _ *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {

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
	client.Client
}

func (u *recreatePodUpdater) PrepareAndFilterPodUpdate(podToUpdate []*PodUpdateInfo, resources *collasetutils.RelatedResources, ownedIDs map[int]*appsv1alpha1.ContextDetail, podCh chan *PodUpdateInfo) (bool, *time.Duration, error) {
	// 1. prepare Pods to begin PodOpsLifecycle, filter updated revision pods or is already during pods
	filterPods(podToUpdate, podCh)
	// 2. begin podOpsLifecycle parallel
	var recordedRequeueAfter *time.Duration
	succCount, err := controllerutils.SlowStartBatch(len(podCh), controllerutils.SlowStartInitialBatchSize, false, func(int, error) error {
		podInfo := <-podCh
		// fulfill Pod update information
		if err := u.FulfillPodUpdatedInfo(resources.UpdatedRevision, podInfo); err != nil {
			return fmt.Errorf("fail to analyse pod %s/%s in-place update support: %s", podInfo.Namespace, podInfo.Name, err)
		}

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
		return updating, nil, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus, appsv1alpha1.CollaSetUpdate, nil, "Updated", "")
	}
	// 3. judge whether it need to update podContext
	recordedRequeueAfter, err = filterPodsAndUpdatePodContext(u.collaSet, podToUpdate, u.recorder, ownedIDs, resources, podCh, u.Client)
	if err != nil {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetScale, err, "UpdateFailed",
			fmt.Sprintf("fail to update Context for updating: %s", err))
		return updating, recordedRequeueAfter, err
	} else {
		collasetutils.AddOrUpdateCondition(resources.NewStatus,
			appsv1alpha1.CollaSetScale, nil, "UpdateFailed", "")
	}
	return updating, recordedRequeueAfter, nil
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

func (u *recreatePodUpdater) FinishUpdatePod(podInfo *PodUpdateInfo) error {
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

type replaceUpdatePodUpdater struct {
	collaSet   *appsv1alpha1.CollaSet
	ctx        context.Context
	podControl podcontrol.Interface
	recorder   record.EventRecorder
	client.Client
}

func (u *replaceUpdatePodUpdater) PrepareAndFilterPodUpdate(podToUpdate []*PodUpdateInfo, _ *collasetutils.RelatedResources, _ map[int]*appsv1alpha1.ContextDetail, podCh chan *PodUpdateInfo) (bool, *time.Duration, error) {
	// The pod is in a "replaceUpdate" state, only filter these pods that already updated revision.
	for i, podInfo := range podToUpdate {
		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged {
			continue
		}
		podCh <- podToUpdate[i]
	}
	return false, nil, nil
}

func (u *replaceUpdatePodUpdater) FulfillPodUpdatedInfo(updatedRevision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) (err error) {
	// when replaceUpdate, inPlaceUpdateSupport and onlyMetadataChanged always false
	// judge replace pair new pod is updated revision, if not, delete.
	podUpdateInfo.analysed = true
	if podUpdateInfo.replacePairNewPodInfo != nil {
		replacePairNewPod := podUpdateInfo.replacePairNewPodInfo.Pod
		newPodRevision, exist := replacePairNewPod.Labels[appsv1.ControllerRevisionHashLabelKey]
		if exist && newPodRevision == updatedRevision.Name {
			return
		}
		u.recorder.Eventf(podUpdateInfo.Pod,
			corev1.EventTypeNormal,
			"ReplaceUpdatePod",
			"label to-delete on new pair pod %s/%s because it is not updated revision, current revision: %s, updated revision: %s",
			replacePairNewPod.Namespace,
			replacePairNewPod.Name,
			newPodRevision,
			updatedRevision.Name)
		patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%d"}}}`, appsv1alpha1.PodDeletionIndicationLabelKey, time.Now().UnixNano())))
		if err = u.Patch(u.ctx, podUpdateInfo.replacePairNewPodInfo.Pod, patch); err != nil {
			err = fmt.Errorf("failed to delete replace pair new pod %s/%s %s",
				podUpdateInfo.replacePairNewPodInfo.Namespace, podUpdateInfo.replacePairNewPodInfo.Name, err)
			return
		}
		return
	}

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
		return isPodUpdatedServiceAvailable(podUpdateInfo)
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

// prepare Pods to begin PodOpsLifecycle, filter updated revision pods or is already during pods
func filterPods(podToUpdate []*PodUpdateInfo, podCh chan *PodUpdateInfo) {
	for i, podInfo := range podToUpdate {
		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged && !podInfo.PvcTmpHashChanged {
			continue
		}

		if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, podInfo) {
			continue
		}
		podCh <- podToUpdate[i]
	}
}

func filterPodsAndUpdatePodContext(cls *appsv1alpha1.CollaSet, podToUpdate []*PodUpdateInfo, recorder record.EventRecorder,
	ownedIDs map[int]*appsv1alpha1.ContextDetail, resources *collasetutils.RelatedResources, podCh chan *PodUpdateInfo,
	client client.Client) (*time.Duration, error) {
	var recordedRequeueAfter *time.Duration
	// judge whether it need to update podContext
	needUpdateContext := false
	for i := range podToUpdate {
		podInfo := podToUpdate[i]
		requeueAfter, allowed := podopslifecycle.AllowOps(collasetutils.UpdateOpsLifecycleAdapter, realValue(cls.Spec.UpdateStrategy.OperationDelaySeconds), podInfo.Pod)
		if !allowed {
			recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "Pod %s is not allowed to update", commonutils.ObjectKeyString(podInfo.Pod))
			continue
		}
		if requeueAfter != nil {
			recorder.Eventf(podInfo, corev1.EventTypeNormal, "PodUpdateLifecycle", "delay Pod update for %d seconds", requeueAfter.Seconds())
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

		if podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged {
			continue
		}
		// if Pod has not been updated, update it.
		podCh <- podToUpdate[i]
	}

	// 4. mark Pod to use updated revision before updating it.
	if needUpdateContext {
		recorder.Eventf(cls, corev1.EventTypeNormal, "UpdateToPodContext", "try to update ResourceContext for CollaSet")
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return podcontext.UpdateToPodContext(client, cls, ownedIDs)
		})
		return recordedRequeueAfter, err
	}
	return recordedRequeueAfter, nil
}
