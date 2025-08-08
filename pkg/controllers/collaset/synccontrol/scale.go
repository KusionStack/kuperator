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
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/features"
	"kusionstack.io/kuperator/pkg/utils/feature"
)

// getPodsToDelete
// 1. finds number of diff pods from filteredPods to do scaleIn
// 2. finds pods allowed to scale in out of diff
func getPodsToDelete(cls *appsv1alpha1.CollaSet, filteredPods []*collasetutils.PodWrapper, replaceMapping map[string]*collasetutils.PodWrapper, diff int) []*collasetutils.PodWrapper {
	targetsPods := getTargetsDeletePods(filteredPods, replaceMapping)
	// select pods to delete in first round according to diff
	sort.Sort(ActivePodsForDeletion(targetsPods))
	if diff > len(targetsPods) {
		diff = len(targetsPods)
	}

	var needDeletePods []*collasetutils.PodWrapper
	// select pods to delete in second round according to replace, delete, exclude
	for i, pod := range targetsPods {
		// find pods to be scaled in out of diff, if allowed to ops
		_, allowed := podopslifecycle.AllowOps(collasetutils.ScaleInOpsLifecycleAdapter, ptr.Deref(cls.Spec.ScaleStrategy.OperationDelaySeconds, 0), pod)
		if i >= diff && !allowed {
			continue
		}

		//  don't scaleIn exclude pod and its newPod (if exist)
		if pod.ToExclude {
			continue
		}

		if replacePairPod, exist := replaceMapping[pod.Name]; exist && replacePairPod != nil {
			// don't selective scaleIn newPod (and its originPod) until replace finished
			if replacePairPod.ToDelete && !pod.ToDelete {
				continue
			}
			// when scaleIn origin Pod, newPod should be deleted if not service available
			if _, serviceAvailable := replacePairPod.Labels[appsv1alpha1.PodServiceAvailableLabel]; !serviceAvailable {
				needDeletePods = append(needDeletePods, replacePairPod)
			}
		}
		needDeletePods = append(needDeletePods, pod)
	}

	return needDeletePods
}

// getTargetsDeletePods finds pods to choose delete, only finds those (1) replace origin pods, (2) non-exclude pods
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

// Less sort deletion order by: podToDelete > podToExclude > duringScaleIn > others
func (s ActivePodsForDeletion) Less(i, j int) bool {
	l, r := s[i], s[j]

	if l.ToDelete != r.ToDelete {
		return l.ToDelete
	}

	if l.ToExclude != r.ToExclude {
		return l.ToExclude
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

// dealIncludeExcludePods returns pods which are allowed to exclude and include
func (r *RealSyncControl) dealIncludeExcludePods(ctx context.Context, cls *appsv1alpha1.CollaSet, pods []*corev1.Pod) (sets.String, sets.String, error) {
	ownedPods := sets.String{}
	excludePodNames := sets.String{}
	includePodNames := sets.String{}
	for _, pod := range pods {
		ownedPods.Insert(pod.Name)
		if _, exist := pod.Labels[appsv1alpha1.PodExcludeIndicationLabelKey]; exist {
			excludePodNames.Insert(pod.Name)
		}
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

	toExcludePods, notAllowedExcludePods, exErr := r.allowIncludeExcludePods(ctx, cls, excludePodNames.List(), collasetutils.AllowResourceExclude)
	toIncludePods, notAllowedIncludePods, inErr := r.allowIncludeExcludePods(ctx, cls, includePodNames.List(), collasetutils.AllowResourceInclude)
	if notAllowedExcludePods.Len() > 0 {
		r.recorder.Eventf(cls, corev1.EventTypeWarning, "ExcludeNotAllowed", fmt.Sprintf("pods [%v] are not allowed to exclude, please find out the reason from pod's event", notAllowedExcludePods.List()))
	}
	if notAllowedIncludePods.Len() > 0 {
		r.recorder.Eventf(cls, corev1.EventTypeWarning, "IncludeNotAllowed", fmt.Sprintf("pods [%v] are not allowed to include, please find out the reason from pod's event", notAllowedIncludePods.List()))
	}
	return toExcludePods, toIncludePods, controllerutils.AggregateErrors([]error{exErr, inErr})
}

// checkAllowFunc refers to AllowResourceExclude and AllowResourceInclude
type checkAllowFunc func(obj metav1.Object, ownerName, ownerKind string) (bool, string)

// allowIncludeExcludePods try to classify podNames to allowedPods and notAllowedPods, using checkAllowFunc func
func (r *RealSyncControl) allowIncludeExcludePods(ctx context.Context, cls *appsv1alpha1.CollaSet, podNames []string, fn checkAllowFunc) (allowPods, notAllowPods sets.String, err error) {
	allowPods = sets.String{}
	notAllowPods = sets.String{}
	for i := range podNames {
		pod := &corev1.Pod{}
		err = r.client.Get(ctx, types.NamespacedName{Namespace: cls.Namespace, Name: podNames[i]}, pod)
		if errors.IsNotFound(err) {
			notAllowPods.Insert(podNames[i])
			continue
		} else if err != nil {
			r.recorder.Eventf(cls, corev1.EventTypeWarning, "ExcludeIncludeFailed", fmt.Sprintf("failed to find pod %s: %s", podNames[i], err.Error()))
			return
		}

		// check allowance for pod
		if allowed, reason := fn(pod, cls.Name, cls.Kind); !allowed {
			r.recorder.Eventf(pod, corev1.EventTypeWarning, "ExcludeIncludeNotAllowed", fmt.Sprintf("pod is not allowed to exclude/include from/to collaset %s/%s: %s", cls.Namespace, cls.Name, reason))
			notAllowPods.Insert(pod.Name)
			continue
		}

		pvcsAllowed := true
		// check allowance for pvcs
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim == nil {
				continue
			}
			pvc := &corev1.PersistentVolumeClaim{}
			err = r.client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}, pvc)
			// If pvc not found, ignore it. In case of pvc is filtered out by controller-mesh
			if errors.IsNotFound(err) {
				continue
			} else if err != nil {
				r.recorder.Eventf(pod, corev1.EventTypeWarning, "ExcludeIncludeNotAllowed", fmt.Sprintf("failed to check allowed to exclude/include from/to collaset %s/%s: %s", cls.Namespace, cls.Name, err.Error()))
				pvcsAllowed = false
				break
			}
			if allowed, reason := fn(pvc, cls.Name, cls.Kind); !allowed {
				r.recorder.Eventf(pod, corev1.EventTypeWarning, "ExcludeIncludeNotAllowed", fmt.Sprintf("pvc is not allowed to exclude/include from/to collaset %s/%s: %s", cls.Namespace, cls.Name, reason))
				notAllowPods.Insert(pod.Name)
				pvcsAllowed = false
				break
			}
		}
		if pvcsAllowed {
			allowPods.Insert(pod.Name)
		}
	}
	return allowPods, notAllowPods, nil
}

// doIncludeExcludePods do real include and exclude for pods which are allowed to in/exclude
func (r *RealSyncControl) doIncludeExcludePods(ctx context.Context, cls *appsv1alpha1.CollaSet, excludePods, includePods []string, availableContexts []*appsv1alpha1.ContextDetail) error {
	var excludeErrs, includeErrs []error
	_, _ = controllerutils.SlowStartBatch(len(excludePods), controllerutils.SlowStartInitialBatchSize, false, func(idx int, _ error) (err error) {
		defer func() { excludeErrs = append(excludeErrs, err) }()
		return r.excludePod(ctx, cls, excludePods[idx])
	})
	_, _ = controllerutils.SlowStartBatch(len(includePods), controllerutils.SlowStartInitialBatchSize, false, func(idx int, _ error) (err error) {
		defer func() { includeErrs = append(includeErrs, err) }()
		return r.includePod(ctx, cls, includePods[idx], strconv.Itoa(availableContexts[idx].ID))
	})
	return controllerutils.AggregateErrors(append(includeErrs, excludeErrs...))
}

// excludePod try to exclude a pod from collaset
func (r *RealSyncControl) excludePod(ctx context.Context, cls *appsv1alpha1.CollaSet, podName string) error {
	pod := &corev1.Pod{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: cls.Namespace, Name: podName}, pod); err != nil {
		return err
	}
	var pvcs []*corev1.PersistentVolumeClaim
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		pvc := &corev1.PersistentVolumeClaim{}
		err := r.client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}, pvc)
		// If pvc not found, ignore it. In case of pvc is filtered out by controller-mesh
		if errors.IsNotFound(err) {
			continue
		} else if err != nil {
			return err
		}
		pvcs = append(pvcs, pvc)
	}

	pod.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] = "true"
	if err := r.podControl.OrphanPod(cls, pod); err != nil {
		return err
	}
	for i := range pvcs {
		pvcs[i].Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] = "true"
		if err := r.pvcControl.OrphanPvc(cls, pvcs[i]); err != nil {
			return err
		}
	}
	return collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, pod.Name, pod.ResourceVersion)
}

// includePod try to include a pod into collaset
func (r *RealSyncControl) includePod(ctx context.Context, cls *appsv1alpha1.CollaSet, podName, instanceId string) error {
	pod := &corev1.Pod{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: cls.Namespace, Name: podName}, pod); err != nil {
		return err
	}

	var pvcs []*corev1.PersistentVolumeClaim
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		pvc := &corev1.PersistentVolumeClaim{}
		err := r.client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}, pvc)
		// If pvc not found, ignore it. In case of pvc is filtered out by controller-mesh
		if errors.IsNotFound(err) {
			continue
		} else if err != nil {
			return err
		}
		pvcs = append(pvcs, pvc)
	}

	pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] = instanceId
	delete(pod.Labels, appsv1alpha1.PodOrphanedIndicateLabelKey)
	if err := r.podControl.AdoptPod(cls, pod); err != nil {
		return err
	}
	for i := range pvcs {
		pvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey] = instanceId
		delete(pvcs[i].Labels, appsv1alpha1.PodOrphanedIndicateLabelKey)
		if err := r.pvcControl.AdoptPvc(cls, pvcs[i]); err != nil {
			return err
		}
	}
	return collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.Pod, pod.Name, pod.ResourceVersion)
}

// adoptPvcsLeftByRetainPolicy adopts pvcs with respect of "whenDelete=true" pvc retention policy
func (r *RealSyncControl) adoptPvcsLeftByRetainPolicy(ctx context.Context, cls *appsv1alpha1.CollaSet) ([]*corev1.PersistentVolumeClaim, error) {
	ownerSelector := cls.Spec.Selector.DeepCopy()
	if ownerSelector.MatchLabels == nil {
		ownerSelector.MatchLabels = map[string]string{}
	}
	ownerSelector.MatchLabels[appsv1alpha1.ControlledByKusionStackLabelKey] = "true"
	ownerSelector.MatchExpressions = append(ownerSelector.MatchExpressions, metav1.LabelSelectorRequirement{
		Key:      appsv1alpha1.PodOrphanedIndicateLabelKey, // should not be excluded pvcs
		Operator: metav1.LabelSelectorOpDoesNotExist,
	})
	ownerSelector.MatchExpressions = append(ownerSelector.MatchExpressions, metav1.LabelSelectorRequirement{
		Key:      appsv1alpha1.PodInstanceIDLabelKey, // instance-id label should exist
		Operator: metav1.LabelSelectorOpExists,
	})
	ownerSelector.MatchExpressions = append(ownerSelector.MatchExpressions, metav1.LabelSelectorRequirement{
		Key:      appsv1alpha1.PvcTemplateHashLabelKey, // pvc-hash label should exist
		Operator: metav1.LabelSelectorOpExists,
	})

	selector, err := metav1.LabelSelectorAsSelector(ownerSelector)
	if err != nil {
		return nil, err
	}

	orphanedPvcList := &corev1.PersistentVolumeClaimList{}
	if err = r.client.List(ctx, orphanedPvcList, &client.ListOptions{Namespace: cls.Namespace, LabelSelector: selector}); err != nil {
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

// reclaimScaleStrategy updates podToDelete, podToExclude, podToInclude in scaleStrategy
func (r *RealSyncControl) reclaimScaleStrategy(ctx context.Context, deletedPods, excludedPods, includedPods sets.String, cls *appsv1alpha1.CollaSet) error {
	// ReclaimPodScaleStrategy FeatureGate defaults to true
	// Add '--feature-gates=ReclaimPodScaleStrategy=false' to container args, to disable reclaim of podToDelete, podToExclude, podToInclude
	if feature.DefaultFeatureGate.Enabled(features.ReclaimPodScaleStrategy) {
		// reclaim PodToDelete
		toDeletePods := sets.NewString(cls.Spec.ScaleStrategy.PodToDelete...)
		notDeletedPods := toDeletePods.Delete(deletedPods.List()...)
		cls.Spec.ScaleStrategy.PodToDelete = notDeletedPods.List()
		// reclaim PodToExclude
		toExcludePods := sets.NewString(cls.Spec.ScaleStrategy.PodToExclude...)
		notExcludePods := toExcludePods.Delete(excludedPods.List()...)
		cls.Spec.ScaleStrategy.PodToExclude = notExcludePods.List()
		// reclaim PodToInclude
		toIncludePodNames := sets.NewString(cls.Spec.ScaleStrategy.PodToInclude...)
		notIncludePods := toIncludePodNames.Delete(includedPods.List()...)
		cls.Spec.ScaleStrategy.PodToInclude = notIncludePods.List()
		// update cls.spec.scaleStrategy
		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			return r.client.Update(ctx, cls)
		}); err != nil {
			return err
		}
		return collasetutils.ActiveExpectations.ExpectUpdate(cls, expectations.CollaSet, cls.Name, cls.ResourceVersion)
	}
	return nil
}
