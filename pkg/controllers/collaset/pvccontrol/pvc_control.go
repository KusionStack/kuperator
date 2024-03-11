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
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	refmanagerutil "kusionstack.io/operating/pkg/controllers/utils/refmanager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Interface interface {
	GetFilteredPVCs(*appsv1alpha1.CollaSet) ([]*corev1.PersistentVolumeClaim, error)
	CreatePodPvcs(*appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	DeletePodPvcs(*appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	DeletePodUnusedPvcs(*appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	SetPvcsOwnerRef(*appsv1alpha1.CollaSet, []*corev1.PersistentVolumeClaim) ([]*corev1.PersistentVolumeClaim, error)
	ReleasePvcsOwnerRef(*appsv1alpha1.CollaSet, []*corev1.PersistentVolumeClaim) ([]*corev1.PersistentVolumeClaim, error)
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

func (pc *RealPvcControl) GetFilteredPVCs(cls *appsv1alpha1.CollaSet) ([]*corev1.PersistentVolumeClaim, error) {
	var filteredPVCs []*corev1.PersistentVolumeClaim
	ownerSelector := cls.Spec.Selector.DeepCopy()

	if ownerSelector.MatchLabels == nil {
		return filteredPVCs, nil
	}
	selector, err := metav1.LabelSelectorAsSelector(ownerSelector)
	if err != nil {
		return filteredPVCs, err
	}
	pvcList := &corev1.PersistentVolumeClaimList{}
	// list pvcs match label selector of set
	err = pc.client.List(context.TODO(), pvcList, &client.ListOptions{Namespace: cls.Namespace, LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if pvc.DeletionTimestamp == nil {
			filteredPVCs = append(filteredPVCs, pvc)
		}
	}

	// if retain pvcs when collaset is deleted, do not claim pvcs
	if collasetutils.PvcPolicyWhenDelete(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType {
		return pc.ReleasePvcsOwnerRef(cls, filteredPVCs)
	}
	return pc.SetPvcsOwnerRef(cls, filteredPVCs)
}

func (pc *RealPvcControl) CreatePodPvcs(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) error {
	id, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	if !exist {
		return nil
	}

	// provision pvcs related to pod using pvc template
	pvcsMap, err := ProvisionUpdatedPvc(pc.client, cls, id, existingPvcs)
	if err != nil {
		return err
	}

	newVolumes := make([]corev1.Volume, 0, len(*pvcsMap))
	// mount updated pvcs to pod.spec.volumes
	for name, pvc := range *pvcsMap {
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
		if _, ok := (*pvcsMap)[currentVolume.Name]; !ok {
			newVolumes = append(newVolumes, currentVolume)
		}
	}
	pod.Spec.Volumes = newVolumes
	return nil
}

func ProvisionUpdatedPvc(c client.Client, cls *appsv1alpha1.CollaSet, id string, existingPvcs []*corev1.PersistentVolumeClaim) (*map[string]*corev1.PersistentVolumeClaim, error) {
	updatedPvcs, _, err := ClassifyPodPvcs(cls, id, existingPvcs)
	if err != nil {
		return nil, err
	}
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		if _, exist := (*updatedPvcs)[pvcTmp.Name]; exist {
			continue
		}

		// create pvc for pod using pvc template
		claim, err := collasetutils.BuildPvcWithHash(cls, &pvcTmp, id)
		if err != nil {
			return nil, err
		}

		if err := c.Create(context.TODO(), claim); err != nil {
			return nil, fmt.Errorf("fail to create pvc for id %s: %s", id, err)
		} else {
			if err = collasetutils.ActiveExpectations.ExpectCreate(cls, expectations.Pvc, claim.Name); err != nil {
				return nil, err
			}
		}

		if err != nil {
			return nil, err
		}
		(*updatedPvcs)[pvcTmp.Name] = claim
	}
	return updatedPvcs, nil
}

func (pc *RealPvcControl) DeletePodPvcs(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, pvcs []*corev1.PersistentVolumeClaim) error {
	if collasetutils.PvcPolicyWhenScaled(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType {
		return nil
	}
	for _, pvc := range pvcs {
		if pvc.Labels == nil || pod.Labels == nil {
			continue
		}

		if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] != pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
			continue
		}

		// delete pvcs labeled same id with pod
		if err := pc.client.Delete(context.TODO(), pvc); err != nil {
			return err
		} else if err := collasetutils.ActiveExpectations.ExpectDelete(cls, expectations.Pvc, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}

func (pc *RealPvcControl) DeletePodUnusedPvcs(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) error {
	if pod.Labels == nil {
		return nil
	}
	if _, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]; !exist {
		return nil
	}

	id := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	newPvcs, oldPvcs, err := ClassifyPodPvcs(cls, id, existingPvcs)
	if err != nil {
		return err
	}

	//delete pvc which is not claimed in templates
	if err := deleteUnclaimedPvcs(pc.client, cls, oldPvcs); err != nil {
		return err
	}
	// delete old pvc if new pvc is provisioned
	if err := deleteOldPvcs(pc.client, cls, newPvcs, oldPvcs); err != nil {
		return err
	}
	return nil
}

func (pc *RealPvcControl) SetPvcsOwnerRef(cls *appsv1alpha1.CollaSet, pvcs []*corev1.PersistentVolumeClaim) ([]*corev1.PersistentVolumeClaim, error) {
	claimPvcs := make([]*corev1.PersistentVolumeClaim, len(pvcs))
	ownerSelector := cls.Spec.Selector.DeepCopy()
	if ownerSelector.MatchLabels == nil {
		return claimPvcs, nil
	}
	cm, err := refmanagerutil.NewRefManager(pc.client, ownerSelector, cls, pc.scheme)
	if err != nil {
		return claimPvcs, fmt.Errorf("fail to create ref manager: %s", err)
	}

	var candidates = make([]client.Object, len(pvcs))
	for i, pvc := range pvcs {
		candidates[i] = pvc
	}
	claims, err := cm.ClaimOwned(candidates)
	if err != nil {
		return claimPvcs, err
	}
	for i, mt := range claims {
		claimPvcs[i] = mt.(*corev1.PersistentVolumeClaim)
	}

	return claimPvcs, nil
}

func (pc *RealPvcControl) ReleasePvcsOwnerRef(cls *appsv1alpha1.CollaSet, pvcs []*corev1.PersistentVolumeClaim) ([]*corev1.PersistentVolumeClaim, error) {
	ownerSelector := cls.Spec.Selector.DeepCopy()
	if ownerSelector.MatchLabels == nil {
		return pvcs, nil
	}

	cm, err := refmanagerutil.NewRefManager(pc.client, ownerSelector, cls, pc.scheme)
	if err != nil {
		return pvcs, fmt.Errorf("fail to create ref manager: %s", err)
	}

	for _, pvc := range pvcs {
		if err = cm.Release(pvc); err != nil {
			return pvcs, err
		}
	}

	return pvcs, nil
}

func ClassifyPodPvcs(cls *appsv1alpha1.CollaSet, id string, existingPvcs []*corev1.PersistentVolumeClaim) (*map[string]*corev1.PersistentVolumeClaim, *map[string]*corev1.PersistentVolumeClaim, error) {
	newPvcs := map[string]*corev1.PersistentVolumeClaim{}
	oldPvcs := map[string]*corev1.PersistentVolumeClaim{}
	newTmpHash, err := collasetutils.PvcTmpHashMapping(cls.Spec.VolumeClaimTemplates)
	if err != nil {
		return &newPvcs, &oldPvcs, err
	}

	for _, pvc := range existingPvcs {
		if pvc.DeletionTimestamp != nil {
			continue
		}

		if pvc.Labels == nil {
			continue
		}

		if val, exist := pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey]; !exist {
			continue
		} else if val != id {
			continue
		}

		if _, exist := pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]; !exist {
			continue
		}
		hash := pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]
		pvcTmpName, err := collasetutils.ExtractPvcTmpName(cls, pvc)
		if err != nil {
			return nil, nil, err
		}

		// classify into updated and old pvcs
		if newTmpHash[pvcTmpName] == hash {
			newPvcs[pvcTmpName] = pvc
		} else {
			oldPvcs[pvcTmpName] = pvc
		}
	}

	return &newPvcs, &oldPvcs, nil
}

func IsPodPvcTmpChanged(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) (bool, error) {
	// get pvc template hash values
	newHashMapping, err := collasetutils.PvcTmpHashMapping(cls.Spec.VolumeClaimTemplates)
	if err != nil {
		return false, err
	}

	// get existing pvc hash values
	oldHashMapping := map[string]string{}
	for _, pvc := range existingPvcs {
		if pvc.Labels == nil || pod.Labels == nil {
			continue
		}
		if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] != pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
			continue
		}
		if _, exist := pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]; !exist {
			continue
		}
		hash := pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]
		pvcTmpName, err := collasetutils.ExtractPvcTmpName(cls, pvc)
		if err != nil {
			return false, err
		}
		if _, exist := oldHashMapping[pvcTmpName]; !exist {
			oldHashMapping[pvcTmpName] = hash
		} else {
			// skip, do not create new pvc twice
			oldHashMapping[pvcTmpName] = newHashMapping[pvcTmpName]
		}
	}

	// compare the hash new pvc from pvc template
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		if _, exist := oldHashMapping[pvcTmp.Name]; !exist {
			continue
		}
		if oldHashMapping[pvcTmp.Name] != newHashMapping[pvcTmp.Name] {
			return true, nil
		}
	}
	return false, nil
}

func deleteUnclaimedPvcs(c client.Client, cls *appsv1alpha1.CollaSet, oldPvcs *map[string]*corev1.PersistentVolumeClaim) error {
	expectedNames := sets.String{}
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		expectedNames.Insert(pvcTmp.Name)
	}
	for pvcTmpName, pvc := range *oldPvcs {
		if expectedNames.Has(pvcTmpName) {
			continue
		}
		// if pvc is not claimed in pvc templates, delete it
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
		// if new pvc is not ready, keep this pvc
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
