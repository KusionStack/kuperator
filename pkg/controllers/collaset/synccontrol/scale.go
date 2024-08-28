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
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/features"
	"kusionstack.io/kuperator/pkg/utils/feature"
)

func getPodsToDelete(filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper, diff int) []*collasetutils.PodWrapper {
	targetsPods := getTargetsDeletePods(filteredPods, replaceMapping)
	// 1. select pods to delete in first round according to diff
	sort.Sort(ActivePodsForDeletion(targetsPods))
	if diff > len(targetsPods) {
		diff = len(targetsPods)
	}

	var needDeletePods []*collasetutils.PodWrapper
	// 2. select pods to delete in second round according to replace, delete...
	for _, pod := range targetsPods[:diff] {
		if replacePairPod, exist := replaceMapping[pod.Name]; exist && replacePairPod != nil {
			// do not scaleIn new pod until origin pod is deleted, if you want to delete new pod, please delete it by label
			if replacePairPod.ToDelete {
				continue
			}
			// new pod not service available, just scaleIn it
			if _, serviceAvailable := replacePairPod.Labels[appsv1alpha1.PodServiceAvailableLabel]; !serviceAvailable {
				needDeletePods = append(needDeletePods, replacePairPod)
			}
		}
		needDeletePods = append(needDeletePods, pod)
	}

	return needDeletePods
}

// when sort pods to choose delete, only sort (1) replace origin pods, (2) non-exclude pods
func getTargetsDeletePods(filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper) []*collasetutils.PodWrapper {
	targetPods := make([]*collasetutils.PodWrapper, len(replaceMapping))
	index := 0
	for _, pod := range filteredPods {
		if _, exist := replaceMapping[pod.Name]; exist {
			targetPods[index] = pod
			index++
		}
	}

	return targetPods
}

type ActivePodsForDeletion []*collasetutils.PodWrapper

func (s ActivePodsForDeletion) Len() int      { return len(s) }
func (s ActivePodsForDeletion) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s ActivePodsForDeletion) Less(i, j int) bool {
	l, r := s[i], s[j]

	// pods which are indicated by ScaleStrategy.PodToDelete should be deleted before others
	if l.ToDelete != r.ToDelete {
		return l.ToDelete
	}

	// pods which are during scaleInOps should be deleted before those not during
	lDuringScaleIn := podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, l)
	rDuringScaleIn := podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, r)
	if lDuringScaleIn != rDuringScaleIn {
		return lDuringScaleIn
	}

	// TODO consider service available timestamps
	return collasetutils.ComparePod(l.Pod, r.Pod)
}

func (r *RealSyncControl) adoptPvcsLeftByRetainPolicy(ctx context.Context, cls *appsv1alpha1.CollaSet) ([]*corev1.PersistentVolumeClaim, error) {
	orphanedPvcList := &corev1.PersistentVolumeClaimList{}
	selector, err := metav1.LabelSelectorAsSelector(cls.Spec.Selector)
	if err != nil {
		return nil, err
	}
	if err = r.client.List(ctx, orphanedPvcList, &client.ListOptions{Namespace: cls.Namespace,
		LabelSelector: selector}); err != nil {
		return nil, err
	}

	// adopt orphaned pvcs
	var claims []*corev1.PersistentVolumeClaim
	for i := range orphanedPvcList.Items {
		pvc := orphanedPvcList.Items[i]
		if pvc.OwnerReferences != nil && len(pvc.OwnerReferences) > 0 {
			continue
		}
		if pvc.Labels == nil {
			pvc.Labels = make(map[string]string)
		}
		if pvc.Annotations == nil {
			pvc.Annotations = make(map[string]string)
		}

		claims = append(claims, &pvc)
	}
	for i := range claims {
		if err := r.pvcControl.AdoptPvc(cls, claims[i]); err != nil {
			return nil, err
		}
	}
	return claims, nil
}

func (r *RealSyncControl) reclaimScaleStrategy(ctx context.Context, toDeletePodNames sets.String, toExcludePodNames sets.String, toIncludedPodNames sets.String, cls *appsv1alpha1.CollaSet) error {
	// ReclaimPodScaleStrategy FeatureGate defaults to true
	// Add '--feature-gates=ReclaimPodScaleStrategy=false' to container args, to disable reclaim of podToDelete
	if feature.DefaultFeatureGate.Enabled(features.ReclaimPodScaleStrategy) && len(toDeletePodNames) > 0 {
		var newPodToDelete []string
		var newPodToExclude []string
		var newPodToInclude []string

		for _, podName := range cls.Spec.ScaleStrategy.PodToDelete {
			if !toDeletePodNames.Has(podName) {
				newPodToDelete = append(newPodToDelete, podName)
			}
		}
		for podName := range toExcludePodNames {
			newPodToExclude = append(newPodToExclude, podName)

		}
		for podName := range toIncludedPodNames {
			newPodToInclude = append(newPodToInclude, podName)
		}
		// update cls.spec.scaleStrategy
		cls.Spec.ScaleStrategy.PodToDelete = newPodToDelete
		cls.Spec.ScaleStrategy.PodToExclude = newPodToExclude
		cls.Spec.ScaleStrategy.PodToInclude = newPodToInclude
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.client.Update(ctx, cls)
		}); err != nil {
			return err
		}
		return collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.CollaSet, cls.Name, cls.ResourceVersion)
	}
	return nil
}

func (r *RealSyncControl) dealIncludeExcludePods(cls *appsv1alpha1.CollaSet, pods []*corev1.Pod) (sets.String, sets.String, int, error) {
	if len(cls.Spec.ScaleStrategy.PodToExclude) == 0 && len(cls.Spec.ScaleStrategy.PodToInclude) == 0 {
		return nil, nil, 0, nil
	}

	ownedPods := sets.String{}
	excludePodNames := sets.String{}
	includePodNames := sets.String{}
	for _, pod := range pods {
		ownedPods.Insert(pod.Name)
	}

	for _, podName := range cls.Spec.ScaleStrategy.PodToExclude {
		if ownedPods.Has(podName) {
			excludePodNames.Insert(podName)
		}
	}

	for _, podName := range cls.Spec.ScaleStrategy.PodToInclude {
		includePodNames.Insert(podName)
	}

	intersection := excludePodNames.Intersection(includePodNames)
	if len(intersection) > 0 {
		r.recorder.Eventf(cls, corev1.EventTypeWarning, "DupExIncludedPod", "duplicated pods %s in both excluding and including sets", strings.Join(intersection.List(), ", "))
		includePodNames.Delete(intersection.List()...)
		excludePodNames.Delete(intersection.List()...)
	}

	if includePodNames.Len() > 0 {
		for _, pod := range pods {
			if includePodNames.Has(pod.Name) {
				includePodNames.Delete(pod.Name)
			}
		}
	}

	notAllowedCount, err := r.doIncludeExcludePods(excludePodNames, includePodNames)
	return excludePodNames, includePodNames, notAllowedCount, err
}

func (r *RealSyncControl) doIncludeExcludePods(excludePods sets.String, includePods sets.String) (int, error) {
	// exclude: remove ownerReference from pod, pvc

	// include: add ownerReference to pod, pvc; allocate instance-id
	return 0, nil
}

func allowInclude(obj metav1.Object, ownerName, ownerKind string) bool {
	labels := obj.GetLabels()
	ownerRefs := obj.GetOwnerReferences()

	// not controlled by ks manager
	if labels == nil || len(labels) == 0 {
		return false
	} else if val, exist := labels[appsv1alpha1.ControlledByKusionStackLabelKey]; !exist || val != "true" {
		return false
	}

	if ownerRefs != nil && len(ownerRefs) > 0 {
		if controller := metav1.GetControllerOf(obj); controller != nil {
			// controlled by others
			if controller.Name != ownerName || controller.Kind != ownerKind {
				return false
			}
			// currently being owned but not indicate orphan
			if controller.Name == ownerName && controller.Kind == ownerName {
				if _, exist := labels[appsv1alpha1.PodOrphanedIndicateLabelKey]; !exist {
					return false
				}
			}
		}
	}
	return true
}

func allowExclude(obj metav1.Object, ownerName, ownerKind string) bool {
	labels := obj.GetLabels()

	// not controlled by ks manager
	if labels == nil || len(labels) == 0 {
		return false
	} else if val, exist := labels[appsv1alpha1.ControlledByKusionStackLabelKey]; !exist || val != "true" {
		return false
	}

	// not controlled by current collaset
	if controller := metav1.GetControllerOf(obj); controller == nil || controller.Name != ownerName || controller.Kind != ownerKind {
		return false
	}
	return true
}
