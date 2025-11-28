/*
Copyright 2025 The KusionStack Authors.

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

package collaset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	clientutil "kusionstack.io/kube-utils/client"
	"kusionstack.io/kube-xset/api"
	"kusionstack.io/kube-xset/synccontrols"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/collaset/utils"
)

var _ synccontrols.TargetUpdater = &inPlaceIfPossibleUpdater{}

func NewInPlaceIfPossibleUpdater() synccontrols.TargetUpdater {
	return &inPlaceIfPossibleUpdater{}
}

type inPlaceIfPossibleUpdater struct {
	synccontrols.GenericTargetUpdater
}

func (u *inPlaceIfPossibleUpdater) FulfillTargetUpdatedInfo(ctx context.Context, revision *appsv1.ControllerRevision, targetUpdateInfo *synccontrols.TargetUpdateInfo) error {
	// 1. build target from current and updated revision
	// TODO: use cache
	currentTarget, err := synccontrols.NewTargetFrom(u.XsetController, u.XsetLabelAnnoMgr, u.OwnerObject, targetUpdateInfo.CurrentRevision, targetUpdateInfo.ID,
		func(object client.Object) error {
			if decorationAdapter, ok := u.XsetController.(api.DecorationAdapter); ok {
				if fn, err := decorationAdapter.GetDecorationPatcherByRevisions(ctx, u.Client, targetUpdateInfo.TargetWrapper.Object, targetUpdateInfo.TargetWrapper.DecorationCurrentRevisions); err != nil {
					return err
				} else {
					return fn(object)
				}
			}
			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("fail to build Target from current revision %s: %v", targetUpdateInfo.CurrentRevision.GetName(), err.Error())
	}

	// TODO: use cache
	targetUpdateInfo.UpdatedTarget, err = synccontrols.NewTargetFrom(u.XsetController, u.XsetLabelAnnoMgr, u.OwnerObject, targetUpdateInfo.UpdateRevision, targetUpdateInfo.ID,
		func(object client.Object) error {
			if decorationAdapter, ok := u.XsetController.(api.DecorationAdapter); ok {
				if fn, err := decorationAdapter.GetDecorationPatcherByRevisions(ctx, u.Client, targetUpdateInfo.TargetWrapper.Object, targetUpdateInfo.TargetWrapper.DecorationUpdatedRevisions); err != nil {
					return err
				} else {
					return fn(object)
				}
			}
			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("fail to build Target from updated revision %s: %v", targetUpdateInfo.UpdateRevision.GetName(), err.Error())
	}

	if targetUpdateInfo.PvcTmpHashChanged {
		targetUpdateInfo.InPlaceUpdateSupport, targetUpdateInfo.OnlyMetadataChanged = false, false
		return nil
	}

	// TODO check diff
	var imageChangedContainers sets.String
	targetUpdateInfo.InPlaceUpdateSupport, targetUpdateInfo.OnlyMetadataChanged, imageChangedContainers = u.diffPod(currentTarget, targetUpdateInfo.UpdatedTarget)

	if !targetUpdateInfo.InPlaceUpdateSupport {
		return nil
	}

	targetUpdateInfo.UpdatedTarget, err = utils.PatchToPod(currentTarget, targetUpdateInfo.UpdatedTarget, targetUpdateInfo.TargetWrapper.Object)
	if err != nil {
		return err
	}

	var podStatus *PodStatus
	if targetUpdateInfo.OnlyMetadataChanged {
		podStatus = &PodStatus{ContainerStates: nil}
	} else {
		containerCurrentStatusMapping := map[string]*corev1.ContainerStatus{}
		currentPod := targetUpdateInfo.TargetWrapper.Object.(*corev1.Pod)
		for i := range currentPod.Status.ContainerStatuses {
			status := currentPod.Status.ContainerStatuses[i]
			// only store and compare imageID of changed containers
			if imageChangedContainers != nil && imageChangedContainers.Has(status.Name) {
				containerCurrentStatusMapping[status.Name] = &status
			}
		}

		podStatus = &PodStatus{ContainerStates: map[string]*ContainerStatus{}}
		updatedPod := targetUpdateInfo.UpdatedTarget.(*corev1.Pod)
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
	}

	podStatusStr, err := json.Marshal(podStatus)
	if err != nil {
		return err
	}
	anno := targetUpdateInfo.UpdatedTarget.GetAnnotations()
	if anno == nil {
		anno = make(map[string]string)
	}
	anno[appsv1alpha1.LastPodStatusAnnotationKey] = string(podStatusStr)
	targetUpdateInfo.UpdatedTarget.SetAnnotations(anno)
	return nil
}

func (u *inPlaceIfPossibleUpdater) diffPod(currentObj, updatedObj client.Object) (inPlaceSetUpdateSupport, onlyMetadataChanged bool, imageChangedContainers sets.String) {
	currentPod := currentObj.(*corev1.Pod)
	updatedPod := updatedObj.(*corev1.Pod)
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

func (u *inPlaceIfPossibleUpdater) UpgradeTarget(ctx context.Context, targetInfo *synccontrols.TargetUpdateInfo) error {
	if targetInfo.OnlyMetadataChanged || targetInfo.InPlaceUpdateSupport {
		// if target template changes only include metadata or support in-place update, just apply these changes to target directly
		if err := u.TargetControl.UpdateTarget(ctx, targetInfo.UpdatedTarget); err != nil {
			return fmt.Errorf("fail to update Target %s/%s when updating by in-place: %s", targetInfo.GetNamespace(), targetInfo.GetName(), err.Error())
		}
		targetInfo.Object = targetInfo.UpdatedTarget
		u.Recorder.Eventf(targetInfo.Object,
			corev1.EventTypeNormal,
			"UpdateTarget",
			"succeed to update Target %s/%s to from revision %s to revision %s by in-place",
			targetInfo.GetNamespace(), targetInfo.GetName(),
			targetInfo.CurrentRevision.GetName(),
			targetInfo.UpdateRevision.GetName())
		return u.CacheExpectations.ExpectUpdation(clientutil.ObjectKeyString(u.OwnerObject), u.TargetGVK, targetInfo.Object.GetNamespace(), targetInfo.Object.GetName(), targetInfo.Object.GetResourceVersion())
	} else {
		// if target has changes not in-place supported, recreate it
		if err := u.GenericTargetUpdater.RecreateTarget(ctx, targetInfo); err != nil {
			return err
		}
		return u.CacheExpectations.ExpectDeletion(clientutil.ObjectKeyString(u.OwnerObject), u.TargetGVK, targetInfo.Object.GetNamespace(), targetInfo.Object.GetName())
	}
}

func (u *inPlaceIfPossibleUpdater) GetTargetUpdateFinishStatus(_ context.Context, targetUpdateInfo *synccontrols.TargetUpdateInfo) (finished bool, msg string, err error) {
	if !targetUpdateInfo.IsUpdatedRevision || targetUpdateInfo.DecorationChanged {
		return false, "add on not updated", nil
	}

	pod := targetUpdateInfo.TargetWrapper.Object.(*corev1.Pod)
	if pod.Status.ContainerStatuses == nil {
		return false, "no container status", nil
	}

	if pod.Spec.Containers == nil {
		return false, "no container spec", nil
	}

	if len(pod.Spec.Containers) != len(pod.Status.ContainerStatuses) {
		return false, "container status number does not match", nil
	}

	if targetUpdateInfo.GetAnnotations() == nil {
		return false, "no annotations for last container status", nil
	}

	targetLastState := &PodStatus{}
	if lastStateJson, exist := targetUpdateInfo.GetAnnotations()[appsv1alpha1.LastPodStatusAnnotationKey]; !exist {
		return false, "no pod last state annotation", nil
	} else if err := json.Unmarshal([]byte(lastStateJson), targetLastState); err != nil {
		msg := fmt.Sprintf("malformat target last state annotation [%s]: %s", lastStateJson, err.Error())
		return false, msg, errors.New(msg)
	}

	if targetLastState.ContainerStates == nil {
		return true, "pod updated with only metadata changed", nil
	}

	imageMapping := map[string]string{}
	for _, containerSpec := range pod.Spec.Containers {
		imageMapping[containerSpec.Name] = containerSpec.Image
	}

	imageIdMapping := map[string]string{}
	for _, containerStatus := range pod.Status.ContainerStatuses {
		imageIdMapping[containerStatus.Name] = containerStatus.ImageID
	}

	for containerName, lastContainerState := range targetLastState.ContainerStates {
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

type PodStatus struct {
	ContainerStates map[string]*ContainerStatus `json:"containerStates,omitempty"`
}

type ContainerStatus struct {
	LatestImage string `json:"latestImage,omitempty"`
	LastImageID string `json:"lastImageID,omitempty"`
}
