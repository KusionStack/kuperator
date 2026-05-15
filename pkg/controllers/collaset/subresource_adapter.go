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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	xsetapi "kusionstack.io/kube-xset/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/collaset/utils"
)

// PvcAdapter implements SubResourceAdapter for PersistentVolumeClaim management.
var _ xsetapi.SubResourceAdapter = &PvcAdapter{}

// PvcAdapter manages PVC subresources for CollaSet.
type PvcAdapter struct {
	templateLabelKey string
}

// NewPvcAdapter creates a new PVC subresource adapter.
func NewPvcAdapter() *PvcAdapter {
	return &PvcAdapter{
		templateLabelKey: defaultXSetControllerLabelManager[xsetapi.SubResourceTemplateLabelKey],
	}
}

// Meta returns the GroupVersionKind for PersistentVolumeClaim.
func (p *PvcAdapter) Meta() schema.GroupVersionKind {
	return corev1.SchemeGroupVersion.WithKind("PersistentVolumeClaim")
}

// GetTemplates returns PVC templates from the CollaSet spec.
func (p *PvcAdapter) GetTemplates(xset xsetapi.XSetObject) ([]xsetapi.SubResourceTemplate, error) {
	cls := xset.(*appsv1alpha1.CollaSet)
	templates := cls.Spec.VolumeClaimTemplates
	var result []xsetapi.SubResourceTemplate
	for i := range templates {
		result = append(result, xsetapi.SubResourceTemplate{
			Name:     templates[i].Name,
			Template: &templates[i],
		})
	}
	return result, nil
}

// RetainWhenXSetDeleted returns true if PVCs should be retained when CollaSet is deleted.
func (p *PvcAdapter) RetainWhenXSetDeleted(xset xsetapi.XSetObject) bool {
	cls := xset.(*appsv1alpha1.CollaSet)
	return utils.PvcPolicyWhenDelete(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType
}

// RetainWhenXSetScaled returns true if PVCs should be retained when CollaSet is scaled in.
func (p *PvcAdapter) RetainWhenXSetScaled(xset xsetapi.XSetObject) bool {
	cls := xset.(*appsv1alpha1.CollaSet)
	return utils.PvcPolicyWhenScaled(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType
}

// RecreateWhenXSetUpdated returns false - PVCs are only recreated when template spec changes (hash mismatch),
// not on every update operation.
func (p *PvcAdapter) RecreateWhenXSetUpdated(_ xsetapi.XSetObject) bool {
	return false
}

// AttachToTarget attaches PVCs to the Pod by merging PVC volumes into the Pod's spec.volumes.
func (p *PvcAdapter) AttachToTarget(_ context.Context, target client.Object, resources []client.Object) error {
	if len(resources) == 0 {
		return nil
	}

	pod := target.(*corev1.Pod)
	var volumes []corev1.Volume
	for _, res := range resources {
		pvc, ok := res.(*corev1.PersistentVolumeClaim)
		if !ok {
			continue
		}
		templateName := pvc.Labels[p.templateLabelKey]
		volumes = append(volumes, corev1.Volume{
			Name: templateName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
				},
			},
		})
	}

	existingVolumes := pod.Spec.Volumes
	volumeMap := make(map[string]corev1.Volume, len(existingVolumes)+len(volumes))
	for i := range existingVolumes {
		volumeMap[existingVolumes[i].Name] = existingVolumes[i]
	}
	for i := range volumes {
		volumeMap[volumes[i].Name] = volumes[i]
	}

	mergedVolumes := make([]corev1.Volume, 0, len(volumeMap))
	for _, v := range volumeMap { //nolint:gocritic // unavoidable when building slice from map values
		mergedVolumes = append(mergedVolumes, v)
	}

	pod.Spec.Volumes = mergedVolumes
	return nil
}