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

package xcollaset

import (
	"context"
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/kube-utils/xset"
	xsetapi "kusionstack.io/kube-utils/xset/api"
	"kusionstack.io/kube-utils/xset/synccontrols"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/controllers/xcollaset/utils"
)

const (
	controllerName = "collaset-controller"

	preReclaimFinalizer = "apps.kusionstack.io/pre-reclaim"
)

func Add(mgr manager.Manager) error {
	xSetController := &CollaSetController{}
	synccontrols.RegisterInPlaceIfPossibleUpdater(&inPlaceIfPossibleUpdater{})
	return xset.SetUpWithManager(mgr, xSetController)
}

var _ xsetapi.XSetController = &CollaSetController{}

type CollaSetController struct {
	XSetOperation
	XOperation
	LifecycleAdapterGetter
	GetLabelManagerAdapterGetter
	SubResourcePvcAdapter
	DecorationAdapter
}

func (m *CollaSetController) ControllerName() string {
	return controllerName
}

func (m *CollaSetController) FinalizerName() string {
	return preReclaimFinalizer
}

func (m *CollaSetController) XSetMeta() metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: appsv1alpha1.SchemeGroupVersion.String(), Kind: "CollaSet"}
}

func (m *CollaSetController) XMeta() metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String(), Kind: "Pod"}
}

func (m *CollaSetController) NewXSetObject() xsetapi.XSetObject {
	return &appsv1alpha1.CollaSet{}
}

func (m *CollaSetController) NewXObject() client.Object {
	return &corev1.Pod{}
}

func (m *CollaSetController) NewXObjectList() client.ObjectList {
	return &corev1.PodList{}
}

type XSetOperation struct{}

func (s *XSetOperation) GetXSetSpec(xset xsetapi.XSetObject) *xsetapi.XSetSpec {
	xSetSpec := xsetapi.XSetSpec{}
	set := xset.(*appsv1alpha1.CollaSet)
	xSetSpec.Paused = set.Spec.Paused
	xSetSpec.Replicas = set.Spec.Replicas
	xSetSpec.Selector = set.Spec.Selector
	xSetSpec.HistoryLimit = set.Spec.HistoryLimit

	// update strategy
	switch set.Spec.UpdateStrategy.PodUpdatePolicy {
	case appsv1alpha1.CollaSetRecreatePodUpdateStrategyType:
		xSetSpec.UpdateStrategy.UpdatePolicy = xsetapi.XSetRecreateTargetUpdateStrategyType
	case appsv1alpha1.CollaSetReplacePodUpdateStrategyType:
		xSetSpec.UpdateStrategy.UpdatePolicy = xsetapi.XSetReplaceTargetUpdateStrategyType
	case appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType:
		xSetSpec.UpdateStrategy.UpdatePolicy = xsetapi.XSetInPlaceIfPossibleTargetUpdateStrategyType
	case appsv1alpha1.CollaSetInPlaceOnlyPodUpdateStrategyType:
		xSetSpec.UpdateStrategy.UpdatePolicy = xsetapi.XSetInPlaceOnlyTargetUpdateStrategyType
	default:
		xSetSpec.UpdateStrategy.UpdatePolicy = xsetapi.XSetInPlaceIfPossibleTargetUpdateStrategyType
	}
	if set.Spec.UpdateStrategy.RollingUpdate != nil {
		rollingUpdate := xsetapi.RollingUpdateStrategy{}
		if set.Spec.UpdateStrategy.RollingUpdate.ByLabel != nil {
			rollingUpdate.ByLabel = &xsetapi.ByLabel{}
		}
		if set.Spec.UpdateStrategy.RollingUpdate.ByPartition != nil {
			rollingUpdate.ByPartition = &xsetapi.ByPartition{}
			rollingUpdate.ByPartition.Partition = set.Spec.UpdateStrategy.RollingUpdate.ByPartition.Partition
		}
		xSetSpec.UpdateStrategy.RollingUpdate = &rollingUpdate
	}
	// scale strategy
	xSetSpec.ScaleStrategy.TargetToDelete = set.Spec.ScaleStrategy.PodToDelete
	xSetSpec.ScaleStrategy.TargetToExclude = set.Spec.ScaleStrategy.PodToExclude
	xSetSpec.ScaleStrategy.TargetToInclude = set.Spec.ScaleStrategy.PodToInclude
	xSetSpec.ScaleStrategy.Context = set.Spec.ScaleStrategy.Context
	xSetSpec.ScaleStrategy.OperationDelaySeconds = set.Spec.ScaleStrategy.OperationDelaySeconds
	// naming strategy
	if set.Spec.NamingStrategy != nil {
		namingStrategy := &xsetapi.NamingStrategy{}
		switch set.Spec.NamingStrategy.PodNamingSuffixPolicy {
		case appsv1alpha1.PodNamingSuffixPolicyPersistentSequence:
			namingStrategy.TargetNamingSuffixPolicy = xsetapi.TargetNamingSuffixPolicyPersistentSequence
		case appsv1alpha1.PodNamingSuffixPolicyRandom:
			namingStrategy.TargetNamingSuffixPolicy = xsetapi.TargetNamingSuffixPolicyRandom
		default:
			namingStrategy.TargetNamingSuffixPolicy = xsetapi.TargetNamingSuffixPolicyRandom
		}
		xSetSpec.NamingStrategy = namingStrategy
	}
	return &xSetSpec
}

func (s *XSetOperation) GetXSetPatch(object metav1.Object) ([]byte, error) {
	set := object.(*appsv1alpha1.CollaSet)
	dsBytes, err := json.Marshal(set)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	err = json.Unmarshal(dsBytes, &raw)
	if err != nil {
		return nil, err
	}
	objCopy := make(map[string]interface{})
	specCopy := make(map[string]interface{})

	// Create a patch of the CollaSet that replaces spec.template
	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	template["$patch"] = "replace"
	specCopy["template"] = template

	if _, exist := spec["volumeClaimTemplates"]; exist {
		specCopy["volumeClaimTemplates"] = spec["volumeClaimTemplates"]
	}

	objCopy["spec"] = specCopy
	patch, err := json.Marshal(objCopy)
	return patch, err
}

func (s *XSetOperation) GetXSetStatus(object xsetapi.XSetObject) *xsetapi.XSetStatus {
	xSetStatus := xsetapi.XSetStatus{}
	set := object.(*appsv1alpha1.CollaSet)
	xSetStatus.Replicas = set.Status.Replicas
	xSetStatus.ReadyReplicas = set.Status.ReadyReplicas
	xSetStatus.UpdatedReplicas = set.Status.UpdatedReplicas
	xSetStatus.OperatingReplicas = set.Status.OperatingReplicas
	xSetStatus.UpdatedReadyReplicas = set.Status.UpdatedReadyReplicas
	xSetStatus.AvailableReplicas = set.Status.AvailableReplicas
	xSetStatus.UpdatedAvailableReplicas = set.Status.UpdatedAvailableReplicas
	xSetStatus.ScheduledReplicas = set.Status.ScheduledReplicas
	xSetStatus.Conditions = convertClsConditionToMetaCondition(set.Status.Conditions)
	xSetStatus.ObservedGeneration = set.Status.ObservedGeneration
	xSetStatus.CurrentRevision = set.Status.CurrentRevision
	xSetStatus.UpdatedRevision = set.Status.UpdatedRevision
	xSetStatus.CollisionCount = set.Status.CollisionCount
	return &xSetStatus
}

func convertClsConditionToMetaCondition(conditions []appsv1alpha1.CollaSetCondition) []metav1.Condition {
	var newCondition []metav1.Condition
	for i := range conditions {
		newCondition = append(newCondition, metav1.Condition{
			Type:               string(conditions[i].Type),
			Status:             metav1.ConditionStatus(conditions[i].Status),
			LastTransitionTime: conditions[i].LastTransitionTime,
			Reason:             conditions[i].Reason,
			Message:            conditions[i].Message,
		})
	}
	return newCondition
}

func (s *XSetOperation) SetXSetStatus(object xsetapi.XSetObject, xSetStatus *xsetapi.XSetStatus) {
	set := object.(*appsv1alpha1.CollaSet)
	set.Status.Replicas = xSetStatus.Replicas
	set.Status.ReadyReplicas = xSetStatus.ReadyReplicas
	set.Status.UpdatedReplicas = xSetStatus.UpdatedReplicas
	set.Status.OperatingReplicas = xSetStatus.OperatingReplicas
	set.Status.UpdatedReadyReplicas = xSetStatus.UpdatedReadyReplicas
	set.Status.AvailableReplicas = xSetStatus.AvailableReplicas
	set.Status.UpdatedAvailableReplicas = xSetStatus.UpdatedAvailableReplicas
	set.Status.ScheduledReplicas = xSetStatus.ScheduledReplicas
	set.Status.Conditions = convertMetaConditionToClsCondition(xSetStatus.Conditions)
	set.Status.ObservedGeneration = xSetStatus.ObservedGeneration
	set.Status.CurrentRevision = xSetStatus.CurrentRevision
	set.Status.UpdatedRevision = xSetStatus.UpdatedRevision
	set.Status.CollisionCount = xSetStatus.CollisionCount
}

func convertMetaConditionToClsCondition(conditions []metav1.Condition) []appsv1alpha1.CollaSetCondition {
	var newCondition []appsv1alpha1.CollaSetCondition
	for i := range conditions {
		newCondition = append(newCondition, appsv1alpha1.CollaSetCondition{
			Type:               appsv1alpha1.CollaSetConditionType(conditions[i].Type),
			Status:             corev1.ConditionStatus(conditions[i].Status),
			LastTransitionTime: conditions[i].LastTransitionTime,
			Reason:             conditions[i].Reason,
			Message:            conditions[i].Message,
		})
	}
	return newCondition
}

func (s *XSetOperation) UpdateScaleStrategy(ctx context.Context, c client.Client, object xsetapi.XSetObject, scaleStrategy *xsetapi.ScaleStrategy) (err error) {
	set := object.(*appsv1alpha1.CollaSet)
	set.Spec.ScaleStrategy.PodToDelete = scaleStrategy.TargetToDelete
	set.Spec.ScaleStrategy.PodToExclude = scaleStrategy.TargetToExclude
	set.Spec.ScaleStrategy.PodToInclude = scaleStrategy.TargetToInclude
	return c.Update(ctx, object)
}

func (s *XSetOperation) GetXSetTemplatePatcher(_ metav1.Object) func(client.Object) error {
	return func(obj client.Object) error {
		return nil
	}
}

type XOperation struct{}

func (o *XOperation) GetXOpsPriority(_ context.Context, _ client.Client, _ client.Object) (*xsetapi.OpsPriority, error) {
	return &xsetapi.OpsPriority{}, nil
}

func (o *XOperation) GetXObjectFromRevision(revision *appsv1.ControllerRevision) (client.Object, error) {
	pod, err := utils.ApplyPatchFromRevision(&corev1.Pod{}, revision)
	if err != nil {
		return nil, err
	}
	return pod, nil
}

func (o *XOperation) CheckReadyTime(object client.Object) (bool, *metav1.Time) {
	pod := object.(*corev1.Pod)
	if controllerutils.IsPodReady(pod) {
		return true, utils.PodReadyTime(pod)
	}
	return false, &metav1.Time{}
}

func (o *XOperation) CheckAvailable(object client.Object) bool {
	pod := object.(*corev1.Pod)
	return controllerutils.IsPodServiceAvailable(pod)
}

func (o *XOperation) CheckScheduled(object client.Object) bool {
	pod := object.(*corev1.Pod)
	return controllerutils.IsPodScheduled(pod)
}

func (o *XOperation) CheckInactive(object client.Object) bool {
	pod := object.(*corev1.Pod)
	return utils.IsPodInactive(pod)
}

type LifecycleAdapterGetter struct{}

func (l *LifecycleAdapterGetter) GetScaleInOpsLifecycleAdapter() xsetapi.LifecycleAdapter {
	return ScaleInOpsLifecycleAdapter
}

func (l *LifecycleAdapterGetter) GetUpdateOpsLifecycleAdapter() xsetapi.LifecycleAdapter {
	return UpdateOpsLifecycleAdapter
}

type GetLabelManagerAdapterGetter struct{}

func (g *GetLabelManagerAdapterGetter) GetLabelManagerAdapter() xsetapi.XSetLabelAnnotationManager {
	return NewXSetControllerLabelManager()
}
