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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

type PodUpdateInfo struct {
	*utils.PodWrapper

	// indicate if this pod has up-to-date revision from its owner, like CollaSet
	IsUpdatedRevision bool
	// carry the pod's current revision
	CurrentRevision *appsv1.ControllerRevision

	// indicates effected PodDecorations changed
	PodDecorationChanged bool

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
			replacePairNewPodInfo := podUpdateInfoMap[replacePairNewPod.Name]
			replacePairNewPodInfo.isInReplacing = true
			replacePairNewPodInfo.replacePairOriginPodName = originPodName
			originPodInfo.replacePairNewPodInfo = replacePairNewPodInfo
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
	AnalyseAndGetUpdatedPod(revision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) (
		inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error)
	GetPodUpdateFinishStatus(podUpdateInfo *PodUpdateInfo) (bool, string, error)
}

func newPodUpdater(ctx context.Context, client client.Client, cls *appsv1alpha1.CollaSet) PodUpdater {
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType:
		// TODO: recreatePodUpdater
		return &recreatePodUpdater{}
	case appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType:
		// In case of using native K8s, Pod is only allowed to update with container image, so InPlaceOnly policy is
		// implemented with InPlaceIfPossible policy as default for compatibility.
		return &inPlaceIfPossibleUpdater{collaSet: cls, ctx: ctx, Client: client}
	case appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType:
		return &replaceUpdatePodUpdater{collaSet: cls, ctx: ctx, Client: client}
	default:
		return &inPlaceIfPossibleUpdater{collaSet: cls, ctx: ctx, Client: client}
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
	collaSet *appsv1alpha1.CollaSet
	ctx      context.Context
	client.Client
}

func (u *inPlaceIfPossibleUpdater) AnalyseAndGetUpdatedPod(
	updatedRevision *appsv1.ControllerRevision,
	podUpdateInfo *PodUpdateInfo) (
	inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {

	// 1. build pod from current and updated revision
	ownerRef := metav1.NewControllerRef(u.collaSet, appsv1alpha1.GroupVersion.WithKind("CollaSet"))
	// TODO: use cache
	currentPod, err := collasetutils.NewPodFrom(u.collaSet, ownerRef, podUpdateInfo.CurrentRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.CurrentPodDecorations)
	})
	if err != nil {
		err = fmt.Errorf("fail to build Pod from current revision %s: %v", podUpdateInfo.CurrentRevision.Name, err)
		return
	}

	// TODO: use cache
	updatedPod, err = collasetutils.NewPodFrom(u.collaSet, ownerRef, updatedRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.UpdatedPodDecorations)
	})
	if err != nil {
		err = fmt.Errorf("fail to build Pod from updated revision %s: %v", updatedRevision.Name, err)
		return
	}

	// 2. compare current and updated pods. Only pod image and metadata are supported to update in-place
	// TODO: use cache
	inPlaceUpdateSupport, onlyMetadataChanged = u.diffPod(currentPod, updatedPod)
	// 3. if pod has changes more than metadata and image
	if !inPlaceUpdateSupport {
		return false, onlyMetadataChanged, nil, nil
	}

	inPlaceUpdateSupport = true
	updatedPod, err = utils.PatchToPod(currentPod, updatedPod, podUpdateInfo.Pod)

	if onlyMetadataChanged {
		if updatedPod.Annotations != nil {
			delete(updatedPod.Annotations, appsv1alpha1.LastPodStatusAnnotationKey)
		}
	} else {
		containerCurrentStatusMapping := map[string]*corev1.ContainerStatus{}
		for i := range podUpdateInfo.Status.ContainerStatuses {
			status := podUpdateInfo.Status.ContainerStatuses[i]
			containerCurrentStatusMapping[status.Name] = &status
		}

		podStatus := &PodStatus{ContainerStates: map[string]*ContainerStatus{}}
		for _, container := range updatedPod.Spec.Containers {
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
			return inPlaceUpdateSupport, onlyMetadataChanged, updatedPod, err
		}

		if updatedPod.Annotations == nil {
			updatedPod.Annotations = map[string]string{}
		}
		updatedPod.Annotations[appsv1alpha1.LastPodStatusAnnotationKey] = string(podStatusStr)
	}

	return
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

func (u *inPlaceOnlyPodUpdater) AnalyseAndGetUpdatedPod(_ *appsv1.ControllerRevision, _ *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {

	return
}

func (u *inPlaceOnlyPodUpdater) GetPodUpdateFinishStatus(_ *PodUpdateInfo) (finished bool, msg string, err error) {
	return
}

type recreatePodUpdater struct {
}

func (u *recreatePodUpdater) AnalyseAndGetUpdatedPod(_ *appsv1.ControllerRevision, _ *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {
	return false, false, nil, nil
}

func (u *recreatePodUpdater) GetPodUpdateFinishStatus(podInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	// Recreate policy alway treat Pod as update finished
	return podInfo.IsUpdatedRevision && !podInfo.PodDecorationChanged, "", nil
}

type replaceUpdatePodUpdater struct {
	collaSet *appsv1alpha1.CollaSet
	ctx      context.Context
	client.Client
}

func (u *replaceUpdatePodUpdater) AnalyseAndGetUpdatedPod(updatedRevision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {
	// when replaceUpdate, inPlaceUpdateSupport and onlyMetadataChanged always false

	// 1. judge replace pair new pod is updated revision, if not, delete.
	if podUpdateInfo.replacePairNewPodInfo != nil {
		newPodRevision, exist := podUpdateInfo.replacePairNewPodInfo.Pod.Labels[appsv1.ControllerRevisionHashLabelKey]
		if exist && newPodRevision == updatedRevision.Name {
			return
		}
		u.Delete(u.ctx, podUpdateInfo.replacePairNewPodInfo.Pod)
	}

	// 2. build new pod by updatedRevision
	ownerRef := metav1.NewControllerRef(u.collaSet, appsv1alpha1.GroupVersion.WithKind("CollaSet"))
	updatedPod, err = collasetutils.NewPodFrom(u.collaSet, ownerRef, updatedRevision, func(in *corev1.Pod) error {
		return utilspoddecoration.PatchListOfDecorations(in, podUpdateInfo.UpdatedPodDecorations)
	})
	if err != nil {
		err = fmt.Errorf("fail to build Pod from current revision %s: %v", podUpdateInfo.CurrentRevision.Name, err)
	}
	return
}

func (u *replaceUpdatePodUpdater) GetPodUpdateFinishStatus(podUpdateInfo *PodUpdateInfo) (finished bool, msg string, err error) {
	replaceNewPodInfo := podUpdateInfo.replacePairNewPodInfo
	if replaceNewPodInfo == nil {
		return isPodUpdatedServiceAvailable(podUpdateInfo)
	}

	return isPodUpdatedServiceAvailable(replaceNewPodInfo)
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
