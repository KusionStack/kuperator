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
	corev1 "k8s.io/api/core/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	xsetapi "kusionstack.io/kube-xset/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/xcollaset/utils"
)

type SubResourcePvcAdapter struct{}

// RetainPvcWhenXSetDeleted returns true if pvc should be retained when XSet is deleted.
func (p *SubResourcePvcAdapter) RetainPvcWhenXSetDeleted(object xsetapi.XSetObject) bool {
	cls := object.(*appsv1alpha1.CollaSet)
	return utils.PvcPolicyWhenDelete(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType
}

// RetainPvcWhenXSetScaled returns true if pvc should be retained when XSet replicas is scaledIn.
func (p *SubResourcePvcAdapter) RetainPvcWhenXSetScaled(object xsetapi.XSetObject) bool {
	cls := object.(*appsv1alpha1.CollaSet)
	return utils.PvcPolicyWhenScaled(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType
}

// GetXSetPvcTemplate returns pvc template from XSet object.
func (p *SubResourcePvcAdapter) GetXSetPvcTemplate(object xsetapi.XSetObject) []corev1.PersistentVolumeClaim {
	cls := object.(*appsv1alpha1.CollaSet)
	return cls.Spec.VolumeClaimTemplates
}

// GetXSpecVolumes returns spec.volumes from X object.
func (p *SubResourcePvcAdapter) GetXSpecVolumes(object client.Object) []corev1.Volume {
	pod := object.(*corev1.Pod)
	return pod.Spec.Volumes
}

// GetXVolumeMounts returns containers volumeMounts from X (pod) object.
func (p *SubResourcePvcAdapter) GetXVolumeMounts(object client.Object) []corev1.VolumeMount {
	pod := object.(*corev1.Pod)
	var volumeMounts []corev1.VolumeMount
	for _, container := range pod.Spec.Containers {
		for _, v := range container.VolumeMounts {
			volumeMounts = append(volumeMounts, v)
		}
	}
	return volumeMounts
}

// SetXSpecVolumes sets spec.volumes to X object.
func (p *SubResourcePvcAdapter) SetXSpecVolumes(object client.Object, pvcs []corev1.Volume) {
	pod := object.(*corev1.Pod)
	pod.Spec.Volumes = pvcs
}
