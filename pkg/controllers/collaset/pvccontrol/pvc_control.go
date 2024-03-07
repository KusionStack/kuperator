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

package pvccontrol

import (
	"context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	"kusionstack.io/operating/pkg/utils/inject"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Interface interface {
	GetFilteredPVCs(client.Object) ([]*corev1.PersistentVolumeClaim, error)
	CreatePodPvcs(*appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	DeletePodPvcs(*appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	DeletePodUnusedPvcs(*appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
}

type RealPvcControl struct {
	client client.Client
	scheme *runtime.Scheme
}

func NewRealPvcControl(client client.Client, scheme *runtime.Scheme) Interface {
	return &RealPvcControl{
		client: client,
		scheme: scheme,
	}
}

// GetFilteredPVCs returns pvcs owned by collaset
func (pc *RealPvcControl) GetFilteredPVCs(owner client.Object) ([]*corev1.PersistentVolumeClaim, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	err := pc.client.List(context.TODO(), pvcList, &client.ListOptions{Namespace: owner.GetNamespace(),
		FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexOwnerRefUID, string(owner.GetUID()))})
	if err != nil {
		return nil, err
	}

	var filteredPVCs []*corev1.PersistentVolumeClaim
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if pvc.DeletionTimestamp == nil {
			filteredPVCs = append(filteredPVCs, pvc)
		}
	}

	return filteredPVCs, nil
}

// CreatePodPvcs create pvcs for the pod across collaset pvc
// templates; if the latest pvc is created, do not create again
func (pc *RealPvcControl) CreatePodPvcs(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) error {
	id, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	if !exist {
		return nil
	}

	// provision pvcs related to pod using pvc template
	pvcs, err := collasetutils.ProvisionUpdatedPvc(pc.client, cls, id, existingPvcs)
	if err != nil {
		return err
	}

	// mount updated pvcs to pod.spec.volumes
	newVolumes := make([]corev1.Volume, 0, len(*pvcs))
	for name, pvc := range *pvcs {
		volume := corev1.Volume{
			Name: name,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvc.Name,
					ReadOnly:  false,
				},
			},
		}
		newVolumes = append(newVolumes, volume)
	}

	currentVolumes := pod.Spec.Volumes
	for i := range currentVolumes {
		currentVolume := currentVolumes[i]
		if _, ok := (*pvcs)[currentVolume.Name]; !ok {
			newVolumes = append(newVolumes, currentVolume)
		}
	}
	pod.Spec.Volumes = newVolumes

	return nil
}

// DeletePodPvcs delete pvcs related to pod refer instance id
func (pc *RealPvcControl) DeletePodPvcs(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, pvcs []*corev1.PersistentVolumeClaim) error {
	for _, pvc := range pvcs {
		if pvc.Labels == nil || pod.Labels == nil {
			continue
		}

		if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] != pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
			continue
		}

		if err := pc.client.Delete(context.TODO(), pvc); err != nil {
			return err
		} else if err := collasetutils.ActiveExpectations.ExpectDelete(cls, expectations.Pvc, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}

// DeletePodUnusedPvcs delete old pvcs for pod after new
// pvcs is provisioned during pvc template updating; and
// also delete the pvcs that not pvc templates
func (pc *RealPvcControl) DeletePodUnusedPvcs(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) error {
	if pod.Labels == nil {
		return nil
	}
	if _, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]; !exist {
		return nil
	}

	id := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	newPvcs, oldPvcs, err := collasetutils.ClassifyPodPvcs(cls, id, existingPvcs)
	if err != nil {
		return err
	}

	//delete the pvcs which is not matched for pvc templates
	if err := deleteUnMatchedPvcs(pc.client, cls, oldPvcs); err != nil {
		return err
	}
	// delete the old pvc once the new pvc is provisioned, or keep it
	if err := deleteOldPvcs(pc.client, cls, newPvcs, oldPvcs); err != nil {
		return err
	}
	return nil
}

func deleteUnMatchedPvcs(c client.Client, cls *appsv1alpha1.CollaSet, oldPvcs *map[string]*corev1.PersistentVolumeClaim) error {
	expectedNames := sets.String{}
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		expectedNames.Insert(pvcTmp.Name)
	}
	for pvcTmpName, pvc := range *oldPvcs {
		// check whether pvc.Name in pvc templates
		if expectedNames.Has(pvcTmpName) {
			continue
		}
		if err := c.Delete(context.TODO(), pvc); err != nil {
			return err
		} else if err := collasetutils.ActiveExpectations.ExpectDelete(cls, expectations.Pvc, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}

func deleteOldPvcs(c client.Client, cls *appsv1alpha1.CollaSet, newPvcs, oldPvcs *map[string]*corev1.PersistentVolumeClaim) error {
	for pvcTmpName, pvc := range *oldPvcs {
		// keep this pvc if new pvc is not ready
		if _, newPvcExist := (*newPvcs)[pvcTmpName]; !newPvcExist {
			continue
		}
		if err := c.Delete(context.TODO(), pvc); err != nil {
			return err
		} else if err := collasetutils.ActiveExpectations.ExpectDelete(cls, expectations.Pvc, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}
