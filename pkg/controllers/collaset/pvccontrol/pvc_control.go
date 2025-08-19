/*
Copyright 2024 The KusionStack Authors.

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
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	kubeutilsclient "kusionstack.io/kube-utils/client"
	kubeutilsexpectations "kusionstack.io/kube-utils/controller/expectations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	refmanagerutil "kusionstack.io/kuperator/pkg/controllers/utils/refmanager"
	"kusionstack.io/kuperator/pkg/utils/inject"
)

type Interface interface {
	GetFilteredPvcs(context.Context, *appsv1alpha1.CollaSet) ([]*corev1.PersistentVolumeClaim, error)
	CreatePodPvcs(context.Context, *appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	DeletePodPvcs(context.Context, *appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	DeletePodUnusedPvcs(context.Context, *appsv1alpha1.CollaSet, *corev1.Pod, []*corev1.PersistentVolumeClaim) error
	OrphanPvc(*appsv1alpha1.CollaSet, *corev1.PersistentVolumeClaim) error
	AdoptPvc(*appsv1alpha1.CollaSet, *corev1.PersistentVolumeClaim) error
}

type RealPvcControl struct {
	client            client.Client
	scheme            *runtime.Scheme
	cacheExpectations kubeutilsexpectations.CacheExpectationsInterface
}

func NewRealPvcControl(client client.Client, scheme *runtime.Scheme, cacheExpectations kubeutilsexpectations.CacheExpectationsInterface) Interface {
	return &RealPvcControl{
		client:            client,
		scheme:            scheme,
		cacheExpectations: cacheExpectations,
	}
}

func (pc *RealPvcControl) GetFilteredPvcs(ctx context.Context, cls *appsv1alpha1.CollaSet) ([]*corev1.PersistentVolumeClaim, error) {
	// list pvcs using ownerReference
	var filteredPVCs []*corev1.PersistentVolumeClaim
	ownedPvcList := &corev1.PersistentVolumeClaimList{}
	if err := pc.client.List(ctx, ownedPvcList, &client.ListOptions{Namespace: cls.Namespace, FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexOwnerRefUID, string(cls.GetUID()))}); err != nil {
		return nil, err
	}

	for i := range ownedPvcList.Items {
		pvc := &ownedPvcList.Items[i]
		if pvc.DeletionTimestamp == nil {
			filteredPVCs = append(filteredPVCs, pvc)
		}
	}
	return filteredPVCs, nil
}

func (pc *RealPvcControl) CreatePodPvcs(ctx context.Context, cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) error {
	id, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	if !exist {
		return nil
	}

	// provision pvcs related to pod using pvc template, and reuse
	// pvcs if "instance-id" and "pvc-template-hash" label matched
	pvcsMap, err := pc.provisionUpdatedPvc(pc.client, ctx, cls, id, existingPvcs)
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

func (pc *RealPvcControl) provisionUpdatedPvc(c client.Client, ctx context.Context, cls *appsv1alpha1.CollaSet, id string, existingPvcs []*corev1.PersistentVolumeClaim) (*map[string]*corev1.PersistentVolumeClaim, error) {
	updatedPvcs, _, err := classifyPodPvcs(cls, id, existingPvcs)
	if err != nil {
		return nil, err
	}
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		// reuse pvc
		if _, exist := (*updatedPvcs)[pvcTmp.Name]; exist {
			continue
		}

		// create new pvc
		claim, err := collasetutils.BuildPvcWithHash(cls, &pvcTmp, id)
		if err != nil {
			return nil, err
		}

		if err := c.Create(ctx, claim); err != nil {
			return nil, fmt.Errorf("fail to create pvc for id %s: %w", id, err)
		} else {
			if err = pc.cacheExpectations.ExpectCreation(kubeutilsclient.ObjectKeyString(cls), collasetutils.PVCGVK, claim.Namespace, claim.Name); err != nil {
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

func (pc *RealPvcControl) DeletePodPvcs(ctx context.Context, cls *appsv1alpha1.CollaSet, pod *corev1.Pod, pvcs []*corev1.PersistentVolumeClaim) error {
	for _, pvc := range pvcs {
		if pvc.Labels == nil || pod.Labels == nil {
			continue
		}

		if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] != pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
			continue
		}

		// delete pvcs labeled same id with pod
		if err := pc.client.Delete(ctx, pvc); err != nil {
			return err
		} else if err := pc.cacheExpectations.ExpectDeletion(kubeutilsclient.ObjectKeyString(cls), collasetutils.PVCGVK, pvc.Namespace, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}

func (pc *RealPvcControl) DeletePodUnusedPvcs(ctx context.Context, cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) error {
	if pod.Labels == nil {
		return nil
	}
	if _, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]; !exist {
		return nil
	}

	id := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	newPvcs, oldPvcs, err := classifyPodPvcs(cls, id, existingPvcs)
	if err != nil {
		return err
	}

	mountedPvcNames := sets.String{}
	for _, container := range pod.Spec.Containers {
		for _, v := range container.VolumeMounts {
			mountedPvcNames.Insert(v.Name)
		}
	}

	// delete pvc which is not claimed in templates
	if err := pc.deleteUnclaimedPvcs(pc.client, ctx, cls, oldPvcs, mountedPvcNames); err != nil {
		return err
	}

	// delete old pvc if new pvc is provisioned and WhenScaled is "Delete"
	if collasetutils.PvcPolicyWhenScaled(cls) == appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType {
		return pc.deleteOldPvcs(pc.client, ctx, cls, newPvcs, oldPvcs)
	}
	return nil
}

func (pc *RealPvcControl) OrphanPvc(cls *appsv1alpha1.CollaSet, pvc *corev1.PersistentVolumeClaim) error {
	if cls.Spec.Selector.MatchLabels == nil {
		return nil
	}
	cm, err := refmanagerutil.NewRefManager(pc.client, cls.Spec.Selector, cls, pc.scheme)
	if err != nil {
		return fmt.Errorf("fail to create ref manager: %w", err)
	}

	if pvc.Labels == nil {
		pvc.Labels = make(map[string]string)
	}
	if pvc.Annotations == nil {
		pvc.Annotations = make(map[string]string)
	}
	return cm.Release(pvc)
}

func (pc *RealPvcControl) AdoptPvc(cls *appsv1alpha1.CollaSet, pvc *corev1.PersistentVolumeClaim) error {
	if cls.Spec.Selector.MatchLabels == nil {
		return nil
	}
	cm, err := refmanagerutil.NewRefManager(pc.client, cls.Spec.Selector, cls, pc.scheme)
	if err != nil {
		return fmt.Errorf("fail to create ref manager: %w", err)
	}

	_, err = cm.ClaimOwned([]client.Object{pvc})
	if err != nil {
		return err
	}
	return nil
}

// classify pvcs into old and new ones
func classifyPodPvcs(cls *appsv1alpha1.CollaSet, id string, existingPvcs []*corev1.PersistentVolumeClaim) (*map[string]*corev1.PersistentVolumeClaim, *map[string]*corev1.PersistentVolumeClaim, error) {
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

	// get existing pod pvcs hash values
	existingPvcHash := map[string]string{}
	for _, pvc := range existingPvcs {
		if pvc.Labels == nil || pod.Labels == nil {
			continue
		}
		if pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] != pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
			continue
		}
		if _, exist := pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]; !exist {
			continue
		}
		existingPvcHash[pvc.Name] = pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]
	}

	// check mounted pvcs changed
	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil || volume.PersistentVolumeClaim.ClaimName == "" {
			continue
		}
		pvcName := volume.PersistentVolumeClaim.ClaimName
		TmpName := volume.Name
		if newHashMapping[TmpName] != existingPvcHash[pvcName] {
			return true, nil
		}
	}
	return false, nil
}

func (pc *RealPvcControl) deleteUnclaimedPvcs(c client.Client, ctx context.Context, cls *appsv1alpha1.CollaSet, oldPvcs *map[string]*corev1.PersistentVolumeClaim, mountedPvcNames sets.String) error {
	expectedNames := sets.String{}
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		expectedNames.Insert(pvcTmp.Name)
	}
	for pvcTmpName, pvc := range *oldPvcs {
		// if pvc is still mounted on pod, keep it
		if mountedPvcNames.Has(pvcTmpName) {
			continue
		}
		// if pvc is claimed in pvc templates, keep it
		if expectedNames.Has(pvcTmpName) {
			continue
		}
		if err := c.Delete(ctx, pvc); err != nil {
			return err
		} else if err := pc.cacheExpectations.ExpectDeletion(kubeutilsclient.ObjectKeyString(cls), collasetutils.PVCGVK, pvc.Namespace, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}

func (pc *RealPvcControl) deleteOldPvcs(c client.Client, ctx context.Context, cls *appsv1alpha1.CollaSet, newPvcs, oldPvcs *map[string]*corev1.PersistentVolumeClaim) error {
	for pvcTmpName, pvc := range *oldPvcs {
		if _, newPvcExist := (*newPvcs)[pvcTmpName]; !newPvcExist {
			continue
		}
		if err := c.Delete(ctx, pvc); err != nil {
			return err
		} else if err := pc.cacheExpectations.ExpectDeletion(kubeutilsclient.ObjectKeyString(cls), collasetutils.PVCGVK, pvc.Namespace, pvc.Name); err != nil {
			return err
		}
	}
	return nil
}
