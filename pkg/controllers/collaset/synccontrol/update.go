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
	"encoding/json"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/kafed/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/kafed/pkg/controllers/utils"
	"kusionstack.io/kafed/pkg/controllers/utils/podopslifecycle"
)

type PodUpdateInfo struct {
	*utils.PodWrapper

	// indicate if this pod has up-to-date revision from its owner, like CollaSet
	IsUpdatedRevision bool
	// carry the pod's current revision
	CurrentRevision *appsv1.ControllerRevision

	// indicates the PodOpsLifecycle is started.
	isDuringOps bool
}

func attachPodUpdateInfo(pods []*collasetutils.PodWrapper, revisions []*appsv1.ControllerRevision, updatedRevision *appsv1.ControllerRevision) []*PodUpdateInfo {
	podUpdateInfoList := make([]*PodUpdateInfo, len(pods))

	for i, pod := range pods {
		updateInfo := &PodUpdateInfo{PodWrapper: pod}

		// decide this pod current revision, or nil if not indicated
		if pod.Labels != nil {
			currentRevisionName, exist := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
			if exist {
				if currentRevisionName == updatedRevision.Name {
					updateInfo.IsUpdatedRevision = true
					updateInfo.CurrentRevision = updatedRevision
				} else {
					updateInfo.IsUpdatedRevision = false
					for _, rv := range revisions {
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

	return podUpdateInfoList
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
			podToUpdate = append(podToUpdate, podInfos[i])
		}
	}

	return podToUpdate
}

func decidePodToUpdateByPartition(cls *appsv1alpha1.CollaSet, podInfos []*PodUpdateInfo) (podToUpdate []*PodUpdateInfo) {
	if cls.Spec.UpdateStrategy.RollingUpdate == nil ||
		cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition == nil {
		return podInfos
	}

	ordered := orderByDefault(podInfos)
	sort.Sort(ordered)

	partition := int(*cls.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition)
	return podInfos[:partition]
}

type orderByDefault []*PodUpdateInfo

func (o orderByDefault) Len() int {
	return len(o)
}

func (o orderByDefault) Swap(i, j int) { o[i], o[j] = o[j], o[i] }

func (o orderByDefault) Less(i, j int) bool {
	l, r := o[i], o[j]
	if l.IsUpdatedRevision && !r.IsUpdatedRevision {
		return true
	}

	if !l.IsUpdatedRevision && r.IsUpdatedRevision {
		return false
	}

	if l.isDuringOps && !r.isDuringOps {
		return true
	}

	if !l.isDuringOps && r.isDuringOps {
		return false
	}

	return controllerutils.ComparePod(l.Pod, r.Pod)
}

type PodUpdater interface {
	AnalyseAndGetUpdatedPod(cls *appsv1alpha1.CollaSet, revision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error)
	GetPodUpdateFinishStatus(pod *corev1.Pod) (bool, string, error)
}

func newPodUpdater(cls *appsv1alpha1.CollaSet) PodUpdater {
	switch cls.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType:
		return &RecreatePodUpdater{}
	case appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType:
		// In case of using native K8s, Pod is only allowed to update with container image, so InPlaceOnly policy is
		// implemented with InPlaceIfPossible policy as default for compatibility.
		return &InPlaceIfPossibleUpdater{}
	default:
		return &InPlaceIfPossibleUpdater{}
	}
}

type PodStatus struct {
	ContainerStates map[string]*ContainerStatus `json:"containerStates,omitempty"`
}

type ContainerStatus struct {
	LatestImage string `json:"latestImage,omitempty"`
	LastImageID string `json:"lastImageID,omitempty"`
}

type InPlaceIfPossibleUpdater struct {
}

func (u *InPlaceIfPossibleUpdater) AnalyseAndGetUpdatedPod(cls *appsv1alpha1.CollaSet, updatedRevision *appsv1.ControllerRevision, podUpdateInfo *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {
	// 1. build pod from current and updated revision
	ownerRef := metav1.NewControllerRef(cls, appsv1alpha1.GroupVersion.WithKind("CollaSet"))
	// TODO: use cache
	currentPod, err := controllerutils.NewPodFrom(cls, ownerRef, podUpdateInfo.CurrentRevision)
	if err != nil {
		return false, false, nil, fmt.Errorf("fail to build Pod from current revision %s: %s", podUpdateInfo.CurrentRevision.Name, err)
	}

	// TODO: use cache
	updatedPod, err = controllerutils.NewPodFrom(cls, ownerRef, updatedRevision)
	if err != nil {
		return false, false, nil, fmt.Errorf("fail to build Pod from updated revision %s: %s", updatedRevision.Name, err)
	}

	// 2. compare current and updated pods. Only pod image and metadata are supported to update in-place
	// TODO: use cache
	inPlaceUpdateSupport, onlyMetadataChanged = u.diffPod(currentPod, updatedPod)
	// 2.1 if pod has changes more than metadata and image
	if !inPlaceUpdateSupport {
		return false, onlyMetadataChanged, nil, nil
	}

	inPlaceUpdateSupport = true
	updatedPod, err = controllerutils.PatchToPod(currentPod, updatedPod, podUpdateInfo.Pod)

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

func (u *InPlaceIfPossibleUpdater) diffPod(currentPod, updatedPod *corev1.Pod) (inPlaceSetUpdateSupport bool, onlyMetadataChanged bool) {
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

func (u *InPlaceIfPossibleUpdater) GetPodUpdateFinishStatus(pod *corev1.Pod) (finished bool, msg string, err error) {
	if pod.Status.ContainerStatuses == nil {
		return false, "no container status", nil
	}

	if pod.Spec.Containers == nil {
		return false, "no container spec", nil
	}

	if len(pod.Spec.Containers) != len(pod.Status.ContainerStatuses) {
		return false, "container status number does not match", nil
	}

	if pod.Annotations == nil {
		return true, "no annotations for last container status", nil
	}

	podLastState := &PodStatus{}
	if lastStateJson, exist := pod.Annotations[appsv1alpha1.LastPodStatusAnnotationKey]; !exist {
		return true, "no pod last state annotation", nil
	} else if err := json.Unmarshal([]byte(lastStateJson), podLastState); err != nil {
		msg := fmt.Sprintf("malformat pod last state annotation [%s]: %s", lastStateJson, err)
		return false, msg, fmt.Errorf(msg)
	}

	if podLastState.ContainerStates == nil {
		return true, "empty last container state recorded", nil
	}

	imageMapping := map[string]string{}
	for _, containerSpec := range pod.Spec.Containers {
		imageMapping[containerSpec.Name] = containerSpec.Image
	}

	imageIdMapping := map[string]string{}
	for _, containerStatus := range pod.Status.ContainerStatuses {
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
type InPlaceOnlyPodUpdater struct {
}

func (u *InPlaceOnlyPodUpdater) AnalyseAndGetUpdatedPod(_ *appsv1alpha1.CollaSet, _ *appsv1.ControllerRevision, _ *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {

	return
}

func (u *InPlaceOnlyPodUpdater) GetPodUpdateFinishStatus(_ *corev1.Pod) (finished bool, msg string, err error) {
	return
}

type RecreatePodUpdater struct {
}

func (u *RecreatePodUpdater) AnalyseAndGetUpdatedPod(_ *appsv1alpha1.CollaSet, _ *appsv1.ControllerRevision, _ *PodUpdateInfo) (inPlaceUpdateSupport bool, onlyMetadataChanged bool, updatedPod *corev1.Pod, err error) {
	return false, false, nil, nil
}

func (u *RecreatePodUpdater) GetPodUpdateFinishStatus(_ *corev1.Pod) (finished bool, msg string, err error) {
	// Recreate policy alway treat Pod as update finished
	return true, "", nil
}
