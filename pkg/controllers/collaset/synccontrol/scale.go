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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/features"
	"kusionstack.io/operating/pkg/utils/feature"
)

func getPodsToDelete(filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper, diff int) []*collasetutils.PodWrapper {
	targetsPods := getTargetsDeletePods(filteredPods, replaceMapping)
	sort.Sort(ActivePodsForDeletion(targetsPods))
	if diff > len(targetsPods) {
		diff = len(targetsPods)
	}

	needDeletePods := targetsPods[:diff]
	for _, pod := range needDeletePods {
		if replacePairPod, exist := replaceMapping[pod.Name]; exist && replacePairPod != nil {
			needDeletePods = append(needDeletePods, replacePairPod)
		}
	}

	return needDeletePods
}

// when sort pods to choose delete, only sort (1) replace origin pods, (2) non-exclude pods
func getTargetsDeletePods(filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper) []*collasetutils.PodWrapper {
	targetPods := make([]*collasetutils.PodWrapper, len(replaceMapping))
	index := 0
	for _, pod := range filteredPods {
		if _, exist := replaceMapping[pod.Name]; exist && !pod.ToExclude {
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

	return collasetutils.ComparePod(l.Pod, r.Pod)
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

func (r *RealSyncControl) dealIncludeExcludePods(cls *appsv1alpha1.CollaSet, pods []*corev1.Pod, resources *collasetutils.RelatedResources) (sets.String, sets.String, bool, error) {
	if len(cls.Spec.ScaleStrategy.PodToExclude) == 0 && len(cls.Spec.ScaleStrategy.PodToInclude) == 0 {
		return nil, nil, false, nil
	}

	ownedPods := sets.String{}
	for _, pod := range pods {
		ownedPods.Insert(pod.Name)
	}

	excludePodNames := sets.String{}
	for _, podName := range cls.Spec.ScaleStrategy.PodToExclude {
		if ownedPods.Has(podName) {
			excludePodNames.Insert(podName)
		}
	}

	includePodNames := sets.String{}
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

	return excludePodNames, includePodNames, false, nil
}
