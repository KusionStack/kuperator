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

package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

// ProvisionUpdatedPvc 1. create pvc for the pod using current
// pvc template; 2. if pvc exists, not create again, reuse it
func ProvisionUpdatedPvc(c client.Client, cls *appsv1alpha1.CollaSet, id string, existingPvcs []*corev1.PersistentVolumeClaim) (*map[string]*corev1.PersistentVolumeClaim, error) {
	updatedPvcs, _, err := ClassifyPodPvcs(cls, id, existingPvcs)
	if err != nil {
		return nil, err
	}
	for _, pvcTmp := range cls.Spec.VolumeClaimTemplates {
		if _, exist := (*updatedPvcs)[pvcTmp.Name]; exist {
			continue
		}

		claim, err := BuildPvcWithHash(cls, &pvcTmp, id)
		if err != nil {
			return nil, err
		}

		if err := c.Create(context.TODO(), claim); err != nil {
			return nil, fmt.Errorf("fail to create pvc for id %s: %s", id, err)
		} else {
			if err = ActiveExpectations.ExpectCreate(cls, expectations.Pvc, claim.Name); err != nil {
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

// BuildPvcWithHash return a pvc labeled with a hash value
func BuildPvcWithHash(cls *appsv1alpha1.CollaSet, pvcTmp *corev1.PersistentVolumeClaim, id string) (*corev1.PersistentVolumeClaim, error) {
	claim := pvcTmp.DeepCopy()
	claim.Name = ""
	claim.GenerateName = fmt.Sprintf("%s-%s-", cls.Name, pvcTmp.Name)
	claim.Namespace = cls.Namespace

	if claim.Labels == nil {
		claim.Labels = map[string]string{}
	}
	for k, v := range cls.Spec.Selector.MatchLabels {
		claim.Labels[k] = v
	}

	hash, err := PvcTmpHash(pvcTmp)
	if err != nil {
		return nil, err
	}
	claim.Labels[appsv1alpha1.PvcTemplateHashLabelKey] = hash
	claim.Labels[appsv1alpha1.PodInstanceIDLabelKey] = id
	return claim, nil
}

// ClassifyPodPvcs classify pvcs into old and new pvcs
// by comparing to the hash of current pvc template
func ClassifyPodPvcs(cls *appsv1alpha1.CollaSet, id string, existingPvcs []*corev1.PersistentVolumeClaim) (*map[string]*corev1.PersistentVolumeClaim, *map[string]*corev1.PersistentVolumeClaim, error) {
	newPvcs := map[string]*corev1.PersistentVolumeClaim{}
	oldPvcs := map[string]*corev1.PersistentVolumeClaim{}
	newTmpHash, err := PvcTmpHashMapping(cls.Spec.VolumeClaimTemplates)
	if err != nil {
		return &newPvcs, &oldPvcs, err
	}
	// filter out the old pvcs
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
		pvcTmpName, err := ExtractPvcTmpName(cls, pvc)
		if err != nil {
			return nil, nil, err
		}

		// filter out the old pvcs
		if newTmpHash[pvcTmpName] == hash {
			newPvcs[pvcTmpName] = pvc
		} else {
			oldPvcs[pvcTmpName] = pvc
		}
	}

	return &newPvcs, &oldPvcs, nil
}

// IsPodPvcTmpChanged check whether pvc template
// being modified, not aware of added or deleted
func IsPodPvcTmpChanged(cls *appsv1alpha1.CollaSet, pod *corev1.Pod, existingPvcs []*corev1.PersistentVolumeClaim) (bool, error) {
	// get pvc template hash values
	newHashMapping, err := PvcTmpHashMapping(cls.Spec.VolumeClaimTemplates)
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
		pvcTmpName, err := ExtractPvcTmpName(cls, pvc)
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

func ExtractPvcTmpName(cls *appsv1alpha1.CollaSet, pvc *corev1.PersistentVolumeClaim) (string, error) {
	lastDashIndex := strings.LastIndex(pvc.Name, "-")
	if lastDashIndex == -1 {
		return "", fmt.Errorf("pvc %s has no postfix", pvc.Name)
	}

	rest := pvc.Name[:lastDashIndex]
	if !strings.HasPrefix(rest, cls.Name+"-") {
		return "", fmt.Errorf("malformed pvc name %s, expected a part of CollaSet name %s", pvc.Name, cls.Name)
	}

	return strings.TrimPrefix(rest, cls.Name+"-"), nil
}

func PvcTmpHash(pvc *corev1.PersistentVolumeClaim) (string, error) {
	bytes, err := json.Marshal(pvc)
	if err != nil {
		return "", fmt.Errorf("fail to marshal pvc template: %s", err)
	}

	hf := fnv.New32()
	if _, err = hf.Write(bytes); err != nil {
		return "", fmt.Errorf("fail to calculate pvc template hash: %s", err)
	}

	return rand.SafeEncodeString(fmt.Sprint(hf.Sum32())), nil
}

func PvcTmpHashMapping(pvcTmps []corev1.PersistentVolumeClaim) (map[string]string, error) {
	pvcHashMapping := map[string]string{}
	for _, pvcTmp := range pvcTmps {
		hash, err := PvcTmpHash(&pvcTmp)
		if err != nil {
			return nil, err
		}
		pvcHashMapping[pvcTmp.Name] = hash
	}
	return pvcHashMapping, nil
}

func PvcPolicyWhenScaled(cls *appsv1alpha1.CollaSet) appsv1alpha1.PersistentVolumeClaimRetentionPolicyType {
	if cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy == nil {
		return appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType
	}

	if cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy.WhenScaled == "" {
		return appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType
	}
	return cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy.WhenScaled
}

func PvcPolicyWhenDelete(cls *appsv1alpha1.CollaSet) appsv1alpha1.PersistentVolumeClaimRetentionPolicyType {
	if cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy == nil {
		return appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType
	}

	if cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy.WhenDeleted == "" {
		return appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType
	}
	return cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy.WhenDeleted
}
