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
	"context"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	imageutils "k8s.io/kubernetes/test/utils/image"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/test/e2e/framework"
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

			By("Scale in 1 pod")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(2)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
		})

		framework.ConformanceIt("recreate pod with same revision", func() {
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

			By("Wait for CollaSet reconciled")
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

		framework.ConformanceIt("Selective delete and scale in pods", func() {
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

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

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

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

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

		framework.ConformanceIt("Exclude include pods", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
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
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("exclude pod and scale in 1 replicas")
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			PodToExclude := pods[0]
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(2)
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToExclude: []string{PodToExclude.Name},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pod is excluded")
			excludedPodID := PodToExclude.Labels[appsv1alpha1.PodInstanceIDLabelKey]
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pods {
					pod := pods[i]
					if pod.Name == PodToExclude.Name {
						return false
					}
				}
				return true
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Check pvc is excluded")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pvcs {
					pvc := pvcs[i]
					if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
						return false
					}
				}
				return true
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Wait for CollaSet reconciled")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check resourceContext")
			Eventually(func() bool {
				resourceContexts, err := tester.ListResourceContextsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resourceContexts[0].Spec.Contexts)).To(Equal(2))
				for _, contextDetail := range resourceContexts[0].Spec.Contexts {
					if excludedPodID == strconv.Itoa(contextDetail.ID) {
						return false
					}
				}
				return true
			}, 10*time.Second, 3*time.Second).Should(BeTrue())

			By("include pod and scale out 1 replicas")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			PodToInclude := PodToExclude
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(3)
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToExclude: []string{},
					PodToInclude: []string{PodToInclude.Name},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pod is included")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pods {
					pod := pods[i]
					if pod.Name == PodToExclude.Name {
						return true
					}
				}
				return false
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Check pvc is included")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pvcs {
					pvc := pvcs[i]
					if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
						return true
					}
				}
				return false
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Wait for CollaSet reconciled")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check resourceContext")
			Eventually(func() bool {
				resourceContexts, err := tester.ListResourceContextsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resourceContexts[0].Spec.Contexts)).To(Equal(3))
				for _, contextDetail := range resourceContexts[0].Spec.Contexts {
					if excludedPodID == strconv.Itoa(contextDetail.ID) {
						return true
					}
				}
				return false
			}, 10*time.Second, 3*time.Second).Should(BeTrue())
		})

		framework.ConformanceIt("Exclude replace origin pod", func() {
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
			// use bad image to mock new replace pod unavailable
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:non-exist"
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())
			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 0, 0, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Label pod to trigger replace")
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			originPod := pods[0]
			Expect(tester.UpdatePod(originPod, func(pod *v1.Pod) {
				pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			})).NotTo(HaveOccurred())
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 0, 0, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Exclude origin pod and scale in 1 replicas")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(1)
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToExclude: []string{originPod.Name},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("No pod is excluded and scale in")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 0, 0, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Mock newPod service available")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pods {
					if pods[i].Labels[appsv1alpha1.PodReplacePairOriginName] == originPod.Name {
						Expect(tester.UpdatePod(pods[i], func(pod *v1.Pod) {
							pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
						})).NotTo(HaveOccurred())
						return true
					}
				}
				return false
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Wait for replace finished and exclude finished")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 0, 0, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check origin pod is deleted")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).Should(BeNil())
			Expect(pods[0].Name).ShouldNot(BeEquivalentTo(originPod.Name))

			By("Check resourceContext")
			resourceContexts, err := tester.ListResourceContextsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(resourceContexts[0].Spec.Contexts)).To(Equal(1))
		})

		framework.ConformanceIt("Exclude replace new pod", func() {
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
			// use bad image to mock new replace pod unavailable
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:non-exist"
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())
			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 0, 0, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Label pod to trigger replace")
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			originPod := pods[0]
			Expect(tester.UpdatePod(originPod, func(pod *v1.Pod) {
				pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			})).NotTo(HaveOccurred())
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 0, 0, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Exclude new pod and scale in 1 replicas")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			var newPod *v1.Pod
			for i := range pods {
				if pods[i].Labels[appsv1alpha1.PodReplacePairOriginName] == originPod.Name {
					newPod = pods[i]
				}
			}
			Expect(newPod).ShouldNot(BeNil())
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(1)
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToExclude: []string{newPod.Name},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("No pod is excluded and scale in")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 0, 0, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Mock newPod service available")
			Eventually(func() bool {
				Expect(tester.UpdatePod(newPod, func(pod *v1.Pod) {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
				})).NotTo(HaveOccurred())
				return true
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Wait for replace finished and exclude finished")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 0, 0, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check new pod is excluded")
			Expect(client.Get(context.Background(), types.NamespacedName{Namespace: newPod.Namespace, Name: newPod.Name}, newPod)).Should(BeNil())
			Expect(len(newPod.OwnerReferences)).To(BeEquivalentTo(0))

			By("Check resourceContext")
			resourceContexts, err := tester.ListResourceContextsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(resourceContexts[0].Spec.Contexts)).To(Equal(1))
		})

		framework.ConformanceIt("Include pod without revision", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
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
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("exclude pod and scale in 1 replicas")
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			PodToExclude := pods[0]
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(2)
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToExclude: []string{PodToExclude.Name},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pod is excluded")
			excludedPodID := PodToExclude.Labels[appsv1alpha1.PodInstanceIDLabelKey]
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pods {
					pod := pods[i]
					if pod.Name == PodToExclude.Name {
						return false
					}
				}
				return true
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Check pvc is excluded")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pvcs {
					pvc := pvcs[i]
					if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
						return false
					}
				}
				return true
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Wait for CollaSet reconciled")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check resourceContext")
			Eventually(func() bool {
				resourceContexts, err := tester.ListResourceContextsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resourceContexts[0].Spec.Contexts)).To(Equal(2))
				for _, contextDetail := range resourceContexts[0].Spec.Contexts {
					if excludedPodID == strconv.Itoa(contextDetail.ID) {
						return false
					}
				}
				return true
			}, 10*time.Second, 3*time.Second).Should(BeTrue())

			By("Remove excluded pod's revision")
			Expect(tester.UpdatePod(PodToExclude, func(pod *v1.Pod) {
				delete(pod.Labels, appsv1.ControllerRevisionHashLabelKey)
			})).Should(BeNil())
			Eventually(func() bool {
				Expect(client.Get(context.Background(), types.NamespacedName{Namespace: PodToExclude.Namespace, Name: PodToExclude.Name}, PodToExclude)).Should(BeNil())
				return PodToExclude.Labels[appsv1.ControllerRevisionHashLabelKey] == ""
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("include pod and scale out 1 replicas")
			PodToInclude := PodToExclude
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Replicas = int32Pointer(3)
				cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
					PodToExclude: []string{},
					PodToInclude: []string{PodToInclude.Name},
				}
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: int32Pointer(3),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check pod is included")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pods {
					pod := pods[i]
					if pod.Name == PodToInclude.Name {
						return true
					}
				}
				return false
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Check pvc is included")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
				Expect(err).Should(BeNil())
				for i := range pvcs {
					pvc := pvcs[i]
					if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
						return true
					}
				}
				return false
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			By("Check resourceContext")
			Eventually(func() bool {
				resourceContexts, err := tester.ListResourceContextsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(resourceContexts[0].Spec.Contexts)).To(Equal(3))
				for _, contextDetail := range resourceContexts[0].Spec.Contexts {
					if excludedPodID == strconv.Itoa(contextDetail.ID) {
						return true
					}
				}
				return false
			}, 10*time.Second, 3*time.Second).Should(BeTrue())

			By("Wait for CollaSet reconciled")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 2, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Update image to nginxNew")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: int32Pointer(0),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Ensure include pod is recreate")
			err = client.Get(context.Background(), types.NamespacedName{Namespace: PodToExclude.Namespace, Name: PodToExclude.Name}, PodToExclude)
			Expect(errors.IsNotFound(err)).To(Equal(true))
		})

		framework.ConformanceIt("PVC retention policy with scale in pods", func() {
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

			By("Wait for PVC provisioned")
			pvcNames := sets.String{}
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
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

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check PVC is reserved")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
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

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check PVC is retained")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
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

			By("Wait for CollaSet reconciled")
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

		framework.ConformanceIt("in-place update main container with sidecar", func() {
			By("Create poddecoration")
			pd := &appsv1alpha1.PodDecoration{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pd",
					Namespace: ns,
				},
				Spec: appsv1alpha1.PodDecorationSpec{
					Selector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      appsv1alpha1.PodInstanceIDLabelKey,
								Operator: metav1.LabelSelectorOpExists,
							},
						},
					},
					Template: appsv1alpha1.PodDecorationPodTemplate{
						Containers: []*appsv1alpha1.ContainerPatch{
							{
								InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
								Container: v1.Container{
									Name:  "sidecar-redis",
									Image: imageutils.GetE2EImage(imageutils.Redis),
								},
							},
						},
					},
				},
			}
			Expect(client.Create(context.TODO(), pd)).NotTo(HaveOccurred())

			cls := tester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{PodUpdatePolicy: appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check sidecar injected")
			Eventually(func() bool {
				pods, err := tester.ListPodsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				return pods[0].Status.ContainerStatuses[1].Name == pd.Spec.Template.Containers[0].Name
			})

			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			oldPodUID := pods[0].UID
			oldContainerStatus := pods[0].Status.ContainerStatuses[0]

			By("Update main image to nginxNew")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
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

			By("Wait for CollaSet reconciled")
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

		framework.ConformanceIt("operationDelaySeconds", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
			cls.Spec.UpdateStrategy = appsv1alpha1.UpdateStrategy{
				OperationDelaySeconds: int32Pointer(10),
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

			By("Wait for CollaSet reconciled")
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

			By("Check operationDelaySeconds")
			var startTime time.Time
			var duration time.Duration
			Eventually(func() bool {
				Expect(client.Get(context.Background(), types.NamespacedName{Namespace: podToUpdate.Namespace, Name: podToUpdate.Name}, podToUpdate)).Should(BeNil())
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				if startedTimestampStr, started := podToUpdate.Labels[labelOperate]; started {
					startedTimestamp, _ := strconv.ParseInt(startedTimestampStr, 10, 64)
					startTime = time.Unix(0, startedTimestamp)
					return true
				}
				return false
			}, 5*time.Second, 3*time.Second).Should(BeTrue())
			Eventually(func() bool {
				Expect(client.Get(context.Background(), types.NamespacedName{Namespace: podToUpdate.Namespace, Name: podToUpdate.Name}, podToUpdate)).Should(BeNil())
				if _, end := podToUpdate.Labels[appsv1alpha1.PodServiceAvailableLabel]; end {
					duration = time.Since(startTime)
					return true
				}
				return false
			}, 12*time.Second, time.Second).Should(BeTrue())
			Expect(duration > time.Duration(*cls.Spec.UpdateStrategy.OperationDelaySeconds)*time.Second).Should(BeTrue())

			By("Wait for update finished")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 3, 3, 3, 1, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
		})

		framework.ConformanceIt("PVC template update", func() {
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

			By("Wait for PVC provisioned")
			oldPvcNames := sets.String{}
			oldRevision := cls.Status.UpdatedRevision
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
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

			By("Update PVC template")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.VolumeClaimTemplates[0].Spec.Resources = v1.ResourceRequirements{
					Requests: v1.ResourceList{
						"storage": resource.MustParse("200m"),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for PVCs and pods recreated")
			Eventually(func() bool {
				if err := tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Status.UpdatedRevision != oldRevision
			}, 30*time.Second, 3*time.Second).Should(Equal(true))
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check new PVCs provisioned")
			Eventually(func() bool {
				pvcs, err := tester.ListPVCForCollaSet(cls)
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

			By("Wait for CollaSet reconciled")
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

		framework.ConformanceIt("finish update if out of partition", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 2, appsv1alpha1.UpdateStrategy{PodUpdatePolicy: appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 2, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Mock traffic finalizer on pods")
			for i := range pods {
				Expect(tester.UpdatePod(pods[i], func(pod *v1.Pod) {
					finalizers := []string{fmt.Sprintf("%s/%s", appsv1alpha1.PodOperationProtectionFinalizerPrefix, "test")}
					pod.Finalizers = finalizers
				})).NotTo(HaveOccurred())
			}

			By("Update CollaSet from v1 to v2")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.Redis)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 0, 0, 0, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Update CollaSet from v2 to v3 with 1 partition")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: int32Pointer(1),
					},
				}
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check update is finished due to out of partition")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 1, 1, 0, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() int {
				pods, err := tester.ListPodsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				duringOpsCount := 0
				for i := range pods {
					if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pods[i]) {
						duringOpsCount++
					}
				}
				return duringOpsCount
			}, 10*time.Second, 3*time.Second).Should(Equal(1))

			By("Remove finalizers from pods")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			for i := range pods {
				Expect(tester.UpdatePod(pods[i], func(pod *v1.Pod) {
					pod.Finalizers = []string{}
				})).NotTo(HaveOccurred())
			}

			By("Wait for update finish")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 2, 2, 2, 1, 2) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
		})

		framework.ConformanceIt("update with cleaning old lifecycle", func() {
			cls := tester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{PodUpdatePolicy: appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType})
			Expect(tester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for status replicas satisfied")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Mock traffic finalizer on pods")
			Expect(tester.UpdatePod(pods[0], func(pod *v1.Pod) {
				if pod.Annotations == nil {
					pod.Annotations = make(map[string]string)
				}
				finalizer := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperationProtectionFinalizerPrefix, "test")
				pod.Finalizers = []string{finalizer}
				pod.Annotations[appsv1alpha1.PodAvailableConditionsAnnotation] = utils.DumpJSON(&appsv1alpha1.PodAvailableConditions{
					ExpectedFinalizers: map[string]string{
						"finalizer/test": finalizer,
					},
				})
			})).NotTo(HaveOccurred())

			By("Update CollaSet from v1 to v2")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.Redis)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 0, 0, 0, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Remove finalizers from pods")
			Expect(tester.UpdatePod(pods[0], func(pod *v1.Pod) {
				pod.Finalizers = []string{}
			})).NotTo(HaveOccurred())

			By("Wait for pod operated")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				_, exist := pods[0].Labels[fmt.Sprintf("%s/%s", appsv1alpha1.PodOperatedLabelPrefix, "collaset")]
				return exist
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Record lifecycle label values")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			lifecycleLabelValues := make(map[string]string)
			for k := range pods[0].Labels {
				if strings.HasPrefix(k, appsv1alpha1.PodOperatedLabelPrefix) {
					lifecycleLabelValues[k] = pods[0].Labels[k]
				}
			}

			By("Update CollaSet from v2 to v3")
			Expect(tester.UpdateCollaSet(cls, func(cls *appsv1alpha1.CollaSet) {
				cls.Spec.Template.Spec.Containers[0].Image = imageutils.GetE2EImage(imageutils.NginxNew)
			})).NotTo(HaveOccurred())

			By("Wait for CollaSet reconciled")
			Eventually(func() bool {
				if err = tester.GetCollaSet(cls); err != nil {
					return false
				}
				return cls.Generation == cls.Status.ObservedGeneration
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Wait for pod operated")
			Eventually(func() bool {
				pods, err = tester.ListPodsForCollaSet(cls)
				Expect(err).NotTo(HaveOccurred())
				_, exist := pods[0].Labels[fmt.Sprintf("%s/%s", appsv1alpha1.PodOperatedLabelPrefix, "collaset")]
				return exist
			}, 10*time.Second, 3*time.Second).Should(Equal(true))

			By("Check old lifecycle are cleaned and new lifecycle created")
			pods, err = tester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			for k := range pods[0].Labels {
				if strings.HasPrefix(k, appsv1alpha1.PodOperatingLabelPrefix) {
					Expect(lifecycleLabelValues[k] == pods[0].Labels[k]).ShouldNot(BeTrue())
				}
			}

			By("Add finalizers to pods")
			Expect(tester.UpdatePod(pods[0], func(pod *v1.Pod) {
				finalizer := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperationProtectionFinalizerPrefix, "test")
				pod.Finalizers = []string{finalizer}
			})).NotTo(HaveOccurred())

			By("Wait for update finish")
			Eventually(func() error { return tester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Remove finalizers from pods")
			Expect(tester.UpdatePod(pods[0], func(pod *v1.Pod) {
				pod.Finalizers = []string{}
			})).NotTo(HaveOccurred())
		})
	})
})

func int32Pointer(val int32) *int32 {
	return &val
}
