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

package xcollaset

import (
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	xsetapi "kusionstack.io/kube-utils/xset/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
)

var (
	UpdateOpsLifecycleAdapter  = &CollaSetUpdateOpsLifecycleAdapter{}
	ScaleInOpsLifecycleAdapter = &CollaSetScaleInOpsLifecycleAdapter{}
)

// CollaSetUpdateOpsLifecycleAdapter tells PodOpsLifecycle the basic workload update ops info
type CollaSetUpdateOpsLifecycleAdapter struct{}

// GetID indicates ID of one PodOpsLifecycle
func (a *CollaSetUpdateOpsLifecycleAdapter) GetID() string {
	return "collaset"
}

// GetType indicates type for an Operator
func (a *CollaSetUpdateOpsLifecycleAdapter) GetType() xsetapi.OperationType {
	return xsetapi.OpsLifecycleTypeUpdate
}

// AllowMultiType indicates whether multiple IDs which have the same Type are allowed
func (a *CollaSetUpdateOpsLifecycleAdapter) AllowMultiType() bool {
	return true
}

// WhenBegin will be executed when begin a lifecycle
func (a *CollaSetUpdateOpsLifecycleAdapter) WhenBegin(_ client.Object) (bool, error) {
	return false, nil
}

// WhenFinish will be executed when finish a lifecycle
func (a *CollaSetUpdateOpsLifecycleAdapter) WhenFinish(pod client.Object) (bool, error) {
	needUpdated := false
	if _, exist := pod.GetLabels()[appsv1alpha1.CollaSetUpdateIndicateLabelKey]; exist {
		delete(pod.GetLabels(), appsv1alpha1.CollaSetUpdateIndicateLabelKey)
		needUpdated = true
	}

	if pod.GetAnnotations() != nil {
		if _, exist := pod.GetAnnotations()[appsv1alpha1.LastPodStatusAnnotationKey]; exist {
			delete(pod.GetAnnotations(), appsv1alpha1.LastPodStatusAnnotationKey)
			needUpdated = true
		}
	}

	return needUpdated, nil
}

// CollaSetScaleInOpsLifecycleAdapter tells PodOpsLifecycle the basic workload scaling in ops info
type CollaSetScaleInOpsLifecycleAdapter struct{}

// GetID indicates ID of one PodOpsLifecycle
func (a *CollaSetScaleInOpsLifecycleAdapter) GetID() string {
	return "collaset"
}

// GetType indicates type for an Operator
func (a *CollaSetScaleInOpsLifecycleAdapter) GetType() xsetapi.OperationType {
	return xsetapi.OpsLifecycleTypeScaleIn
}

// AllowMultiType indicates whether multiple IDs which have the same Type are allowed
func (a *CollaSetScaleInOpsLifecycleAdapter) AllowMultiType() bool {
	return true
}

// WhenBegin will be executed when begin a lifecycle
func (a *CollaSetScaleInOpsLifecycleAdapter) WhenBegin(pod client.Object) (bool, error) {
	return podopslifecycle.WhenBeginDelete(pod)
}

// WhenFinish will be executed when finish a lifecycle
func (a *CollaSetScaleInOpsLifecycleAdapter) WhenFinish(_ client.Object) (bool, error) {
	return false, nil
}
