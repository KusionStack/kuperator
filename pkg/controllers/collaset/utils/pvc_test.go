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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

var _ = Describe("Pvc utils", func() {
	It("test utils", func() {
		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(4),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "foo",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "pvc1",
										MountPath: "/tmp/pvc1",
									},
								},
							},
						},
					},
				},
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
				},
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc1",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"storage": resource.MustParse("100m"),
								},
							},
							AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						},
					},
				},
			},
		}
		pvc, _ := BuildPvcWithHash(cs, &cs.Spec.VolumeClaimTemplates[0], "0")
		pvc.Name = pvc.GenerateName
		pvcTmpHashMapping, _ := PvcTmpHashMapping(cs.Spec.VolumeClaimTemplates)
		pvcTmpHash, _ := PvcTmpHash(&cs.Spec.VolumeClaimTemplates[0])
		pvcTmpName, _ := ExtractPvcTmpName(cs, pvc)
		Expect(pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey]).Should(BeEquivalentTo("0"))
		Expect(pvc.Labels[appsv1alpha1.PvcTemplateHashLabelKey]).Should(BeEquivalentTo(pvcTmpHash))
		Expect(pvcTmpName).Should(BeEquivalentTo(cs.Spec.VolumeClaimTemplates[0].Name))
		Expect(pvcTmpHashMapping[pvcTmpName]).Should(BeEquivalentTo(pvcTmpHash))

		Expect(PvcPolicyWhenScaled(cs)).Should(BeEquivalentTo(appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType))
		Expect(PvcPolicyWhenDelete(cs)).Should(BeEquivalentTo(appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType))
		cs.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{}
		Expect(PvcPolicyWhenScaled(cs)).Should(BeEquivalentTo(appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType))
		Expect(PvcPolicyWhenDelete(cs)).Should(BeEquivalentTo(appsv1alpha1.DeletePersistentVolumeClaimRetentionPolicyType))
		cs.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{
			WhenDeleted: appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
			WhenScaled:  appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
		}
		Expect(PvcPolicyWhenScaled(cs)).Should(BeEquivalentTo(appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType))
		Expect(PvcPolicyWhenDelete(cs)).Should(BeEquivalentTo(appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType))

	})
})

func int32Pointer(val int32) *int32 {
	return &val
}
