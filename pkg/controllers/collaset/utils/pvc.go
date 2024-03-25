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

package utils

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func BuildPvcWithHash(cls *appsv1alpha1.CollaSet, pvcTmp *corev1.PersistentVolumeClaim, id string) (*corev1.PersistentVolumeClaim, error) {
	claim := pvcTmp.DeepCopy()
	claim.Name = ""
	claim.GenerateName = fmt.Sprintf("%s-%s-", cls.Name, pvcTmp.Name)
	claim.Namespace = cls.Namespace
	claim.OwnerReferences = append(claim.OwnerReferences,
		*metav1.NewControllerRef(cls, appsv1alpha1.GroupVersion.WithKind("CollaSet")))

	if claim.Labels == nil {
		claim.Labels = map[string]string{}
	}
	for k, v := range cls.Spec.Selector.MatchLabels {
		claim.Labels[k] = v
	}
	claim.Labels[appsv1alpha1.ControlledByKusionStackLabelKey] = "true"

	hash, err := PvcTmpHash(pvcTmp)
	if err != nil {
		return nil, err
	}
	claim.Labels[appsv1alpha1.PvcTemplateHashLabelKey] = hash
	claim.Labels[appsv1alpha1.PodInstanceIDLabelKey] = id
	return claim, nil
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
