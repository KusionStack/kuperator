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

package apps

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	imageutils "k8s.io/kubernetes/test/utils/image"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/test/e2e/framework"
)

var _ = SIGDescribe("CollaSet", func() {

	f := framework.NewDefaultFramework("collaset")
	var client client.Client
	var ns string
	var clientSet clientset.Interface
	var tester *framework.CollaSetTester
	var randStr string

	BeforeEach(func() {
		clientSet = f.ClientSet
		client = f.Client
		ns = f.Namespace.Name
		tester = framework.NewCollaSetTester(clientSet, client, ns)
		randStr = rand.String(10)
	})

	framework.KusionstackDescribe("CollaSet Scaling", func() {

		framework.ConformanceIt("scales in normal cases", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Scale in 1 replicas")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(2)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
		})

		framework.ConformanceIt("pod recreate pod with same revision", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 2, appsv1alpha1.UpdateStrategy{})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Update CollaSet with 1 partition")
			oldRevision := cls.Status.CurrentRevision
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: int32Pointer(1),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for update finished")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 1, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Delete the old pod")
			var deletePod, updatedPod string
			Eventually(func() bool {
				pods, err := tester.ListPodsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				for i := range pods {
					pod := pods[i]
					if pod.Labels[appsv1.ControllerRevisionHashLabelKey] == cls.Status.UpdatedRevision && updatedPod == "" {
						updatedPod = pod.Name
					} else if pod.Labels[appsv1.ControllerRevisionHashLabelKey] == cls.Status.CurrentRevision && deletePod == "" {
						Expect(tester.UpdatePod(pod, func(pod *v1.Pod) {
							pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = "true"
						})).NotTo(HaveOccurred())
						deletePod = pod.Name
					}
				}
				return deletePod != "" && updatedPod != ""
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for pod deleted and recreated")
			Eventually(func() bool {
				pods, err := tester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				for i := range pods {
					if pods[i].Name == deletePod {
						return false
					}
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 1, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check recreate pod revision")
			Eventually(func() bool {
				pods, err := tester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				for i := range pods {
					if pods[i].Name != updatedPod {
						return pods[i].Labels[appsv1.ControllerRevisionHashLabelKey] == oldRevision
					}
				}
				return false
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
		})

		framework.ConformanceIt("podToDelete", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("selective delete pods")
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			podToDelete := pods[0]
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToDelete: []string{podToDelete.Name},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for selective delete pods finished")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return len(cls.Spec.ScaleStrategy.PodToDelete) == 0
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pod is deleted")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				for i := range pods {
					if pods[i].Name == podToDelete.Name {
						return false
					}
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("selective scale in pods")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			podToDelete = pods[0]
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToDelete: []string{podToDelete.Name},
				}
				cls.Spec.Replicas = int32Pointer(2)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for selective scale in pods finished")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return len(cls.Spec.ScaleStrategy.PodToDelete) == 0
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pod is scaled in")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				for i := range pods {
					if pods[i].Name == podToDelete.Name {
						return false
					}
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
		})

		framework.ConformanceIt("pvc retention policy with scale in pods", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 2, appsv1alpha1.UpdateStrategy{})
			cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{
				WhenScaled: appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
			}
			cls.Spec.VolumeClaimTemplates = []v1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pvc-test",
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								"storage": resource.MustParse("100m"),
							},
						},
						AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
					},
				},
			}
			cls.Spec.Template.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
				{
					MountPath: "/path/to/mount",
					Name:      "pvc-test",
				},
			}
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Wait for pvc provisioned")
			pvcNames := sets.String{}
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCloneSet(cls)
				if err != nil {
					return false
				}
				if len(pvcs) != 2 {
					return false
				}
				for i := range pvcs {
					pvcNames.Insert(pvcs[i].Name)
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("Scale in 1 replicas")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(1)
			})).NotTo(HaveOccurred())
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pvc is reserved")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCloneSet(cls)
				if err != nil {
					return false
				}
				return len(pvcs) == 2
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("Scale out 1 replicas")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(2)
			})).NotTo(HaveOccurred())
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pvc is retained")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCloneSet(cls)
				if err != nil {
					return false
				}
				if len(pvcs) != 2 {
					return false
				}
				for i := range pvcs {
					if !pvcNames.Has(pvcs[i].Name) {
						return false
					}
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
		})
	})

	framework.KusionstackDescribe("CollaSet Updating", func() {

		framework.ConformanceIt("in-place update images with same imageID", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{PodUpdatePolicy: appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			oldPodUID := pods[0].UID
			oldContainerStatus := pods[0].Status.ContainerStatuses[0]

			By("Update image to nginxNew")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for all pods updated and ready")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Verify the PodUID not changed but containerID and imageID changed")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			newPodUID := pods[0].UID
			newContainerStatus := pods[0].Status.ContainerStatuses[0]

			Expect(oldPodUID).Should(Equal(newPodUID))
			Expect(newContainerStatus.ContainerID).NotTo(Equal(oldContainerStatus.ContainerID))
			Expect(newContainerStatus.ImageID).NotTo(Equal(oldContainerStatus.ImageID))
		})

		framework.ConformanceIt("update pods by label", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
			cls.Spec.UpdateStrategy = appsv1alpha1.UpdateStrategy{
				RollingUpdate: &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByLabel: &appsv1alpha1.ByLabel{},
				},
			}
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Update image to nginxNew but pods are not updated")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
			})).NotTo(HaveOccurred())
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 0, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Label pod to trigger update")
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			podToUpdate := pods[0]
			Expect(tester.UpdatePod(podToUpdate, func(pod *v1.Pod) {
				pod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] = "true"
			})).NotTo(HaveOccurred())

			By("Wait for update finished")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 1, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		})

		framework.ConformanceIt("pvc template update", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 2, appsv1alpha1.UpdateStrategy{})
			cls.Spec.VolumeClaimTemplates = []v1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pvc-test",
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								"storage": resource.MustParse("100m"),
							},
						},
						AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
					},
				},
			}
			cls.Spec.Template.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
				{
					MountPath: "/path/to/mount",
					Name:      "pvc-test",
				},
			}
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Wait for pvc provisioned")
			oldPvcNames := sets.String{}
			oldRevision := cls.Status.UpdatedRevision
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCloneSet(cls)
				if err != nil {
					return false
				}
				if len(pvcs) != 2 {
					return false
				}
				for i := range pvcs {
					oldPvcNames.Insert(pvcs[i].Name)
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))

			By("Update pvc template")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.VolumeClaimTemplates[0].Spec.Resources = v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"storage": resource.MustParse("200m"),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for pvcs and pods recreated")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Status.UpdatedRevision != oldRevision
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check new pvcs provisioned")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCloneSet(cls)
				if err != nil {
					return false
				}
				if len(pvcs) != 2 {
					return false
				}
				for i := range pvcs {
					if oldPvcNames.Has(pvcs[i].Name) {
						return false
					}
					if pvcs[i].Spec.Resources.Requests["storage"] != resource.MustParse("200m") {
						return false
					}
				}
				return true
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
		})

		framework.ConformanceIt("replace update by partition", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{PodUpdatePolicy: appsv1alpha1.CollaSetReplacePodUpdateStrategyType})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Update image to nginxNew")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: int32Pointer(3),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet generation consistent")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Update CollaSet by partition")
			for _, partition := range []int32{3, 2, 1, 0} {
				Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
					cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
						ByPartition: &appsv1alpha1.ByPartition{
							Partition: &partition,
						},
					}
				})).NotTo(HaveOccurred())
				Eventually(func() bool {
					if err := tester.GetCollaSet(cls); err != nil {
						return false
					}
					return cls.Generation == cls.Status.ObservedGeneration
				}, 10*time.Second, 3*time.Second).Should(Equal(true))
				Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3-partition, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			}
		})
	})

})

func int32Pointer(val int32) *int32 {
	return &val
}