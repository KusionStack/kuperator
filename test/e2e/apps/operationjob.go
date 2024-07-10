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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/test/e2e/framework"
)

var _ = SIGDescribe("OperationJob", func() {

	f := framework.NewDefaultFramework("operationjob")
	var client client.Client
	var ns string
	var clientSet clientset.Interface
	var clsTester *framework.CollaSetTester
	var ojTester *framework.OperationJobTester
	var randStr string

	BeforeEach(func() {
		clientSet = f.ClientSet
		client = f.Client
		ns = f.Namespace.Name
		clsTester = framework.NewCollaSetTester(clientSet, client, ns)
		ojTester = framework.NewOperationJobTester(clientSet, client, ns)
		randStr = rand.String(10)
	})

	framework.KusionstackDescribe("OperationJob Replacing", func() {

		framework.ConformanceIt("operationjob replace pod", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create replace OperationJob")
			podToReplace := pods[0]
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToReplace.Name,
				},
			})
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for replace OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
		})

		framework.ConformanceIt("operationjob replace by partition", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create replace OperationJob")
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: pods[0].Name,
				}, {
					PodName: pods[1].Name,
				}, {
					PodName: pods[2].Name,
				},
			})
			oj.Spec.Partition = int32Pointer(0)
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for replace OperationJob Pending")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressPending) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Start to replace pod byPartition")
			for _, partition := range []int32{0, 1, 2, 3} {
				Expect(ojTester.UpdateOperationJob(oj, func(oj *appsv1alpha1.OperationJob) {
					oj.Spec.Partition = &partition
				})).NotTo(HaveOccurred())
				Eventually(func() int32 {
					if err := ojTester.GetOperationJob(oj); err != nil {
						return -1
					}
					return oj.Status.SucceededPodCount
				}, 10*time.Second, 3*time.Second).Should(Equal(partition))
			}

			By("Wait for replace OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		})

		framework.ConformanceIt("operationjob replace non-exist pod", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Create replace OperationJob")
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: "non-exist-pod",
				},
			})
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for replace OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check reason for Succeeded")
			Expect(oj.Status.PodDetails[0].Reason).To(Equal(appsv1alpha1.ReasonPodNotFound))
		})

		framework.ConformanceIt("operationjob replace pod parallel", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create 2 OperationJob to replace the same pod")
			podToReplace := pods[0]
			oj1 := ojTester.NewOperationJob("operationjob-"+randStr+"-1", appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToReplace.Name,
				},
			})
			oj2 := ojTester.NewOperationJob("operationjob-"+randStr+"-2", appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToReplace.Name,
				},
			})
			Expect(ojTester.CreateOperationJob(oj1)).NotTo(HaveOccurred())
			Expect(ojTester.CreateOperationJob(oj2)).NotTo(HaveOccurred())

			By("Wait for OperationJobs Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj1, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj2, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check replace new pod of OperationJobs")
			Expect(oj1.Status.PodDetails[0].Message).To(Equal(oj2.Status.PodDetails[0].Message))
		})

		framework.ConformanceIt("delete operationjob to cancel replace", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			// use bad image to mock new replace pod unavailable
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:non-exist"
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 1, 0, 0, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create replace OperationJob")
			podToReplace := pods[0]
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToReplace.Name,
				},
			})
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for replace OperationJob Processing")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressProcessing) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() bool {
				pods, err := clsTester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				return len(pods) == 2
			}, 30*time.Second, 3*time.Second).Should(BeTrue())

			By("Delete OperationJob to cancel replace")
			Expect(ojTester.DeleteOperationJob(oj)).ShouldNot(HaveOccurred())
			Eventually(func() bool {
				return errors.IsNotFound(ojTester.GetOperationJob(oj))
			}, 30*time.Second, 3*time.Second).Should(BeTrue())

			By("Check replace canceled")
			Eventually(func() bool {
				pods, err := clsTester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				if len(pods) != 1 {
					return false
				}
				return pods[0].Name == podToReplace.Name
			}, 30*time.Second, 3*time.Second).Should(BeTrue())
		})

		framework.ConformanceIt("operationjob activeDeadlineSeconds and TTLSecondsAfterFinished for replace", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			// use bad image to mock new replace pod unavailable
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:non-exist"
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 1, 0, 0, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create replace OperationJob")
			podToReplace := pods[0]
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToReplace.Name,
				},
			})
			oj.Spec.ActiveDeadlineSeconds = int32Pointer(10)
			oj.Spec.TTLSecondsAfterFinished = int32Pointer(10)
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for replace OperationJob Failed")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressFailed) }, 20*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Wait for replace OperationJob deleted")
			Eventually(func() bool {
				return errors.IsNotFound(ojTester.GetOperationJob(oj))
			}, 20*time.Second, 3*time.Second).Should(BeTrue())

			By("Check replace canceled")
			Eventually(func() bool {
				pods, err := clsTester.ListPodsForCollaSet(cls)
				if err != nil {
					return false
				}
				if len(pods) != 1 {
					return false
				}
				return pods[0].Name == podToReplace.Name
			}, 30*time.Second, 3*time.Second).Should(BeTrue())
		})
	})

	framework.KusionstackDescribe("OperationJob Recreating", func() {

		framework.ConformanceIt("operationjob recreate pod", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create recreate OperationJob")
			podToRecreate := pods[0]
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionRecreate, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToRecreate.Name,
				},
			})
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for recreate OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check restartCount of pod")
			pods, err = clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			Expect(pods[0].Name).To(Equal(podToRecreate.Name))
			Expect(int(pods[0].Status.ContainerStatuses[0].RestartCount)).To(Equal(1))
		})

		framework.ConformanceIt("operationjob recreate by partition", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 3, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 3, 3, 3, 3, 3) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create recreate OperationJob")
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionRecreate, []appsv1alpha1.PodOpsTarget{
				{
					PodName: pods[0].Name,
				}, {
					PodName: pods[1].Name,
				}, {
					PodName: pods[2].Name,
				},
			})
			oj.Spec.Partition = int32Pointer(0)
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for recreate OperationJob Pending")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressPending) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Start to recreate pod byPartition")
			for _, partition := range []int32{0, 1, 2, 3} {
				Expect(ojTester.UpdateOperationJob(oj, func(oj *appsv1alpha1.OperationJob) {
					oj.Spec.Partition = &partition
				})).NotTo(HaveOccurred())
				Eventually(func() int32 {
					if err := ojTester.GetOperationJob(oj); err != nil {
						return -1
					}
					return oj.Status.SucceededPodCount
				}, 10*time.Second, 3*time.Second).Should(Equal(partition))
			}

			By("Wait for recreate OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check restartCount of pods")
			pods, err = clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			for i := range pods {
				Expect(int(pods[i].Status.ContainerStatuses[0].RestartCount)).To(Equal(1))
			}
		})

		framework.ConformanceIt("operationjob recreate non-exist pod", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Create recreate OperationJob")
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionRecreate, []appsv1alpha1.PodOpsTarget{
				{
					PodName: "non-exist-pod",
				},
			})
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for recreate OperationJob Failed")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressFailed) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check reason for Failed")
			Expect(oj.Status.PodDetails[0].Reason).To(Equal(appsv1alpha1.ReasonPodNotFound))
		})

		framework.ConformanceIt("operationjob activeDeadlineSeconds and TTLSecondsAfterFinished for recreate", func() {
			cls := clsTester.NewCollaSet("collaset-"+randStr, 1, appsv1alpha1.UpdateStrategy{})
			Expect(clsTester.CreateCollaSet(cls)).NotTo(HaveOccurred())

			By("Wait for CollaSet status replicas satisfied")
			Eventually(func() error { return clsTester.ExpectedStatusReplicas(cls, 1, 1, 1, 1, 1) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			pods, err := clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())

			By("Create recreate OperationJob")
			podToRecreate := pods[0]
			oj := ojTester.NewOperationJob("operationjob-"+randStr, appsv1alpha1.OpsActionRecreate, []appsv1alpha1.PodOpsTarget{
				{
					PodName: podToRecreate.Name,
				},
			})
			oj.Spec.TTLSecondsAfterFinished = int32Pointer(10)
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for recreate OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() bool {
				CRR, err := ojTester.GetForOperationJobTester(oj, podToRecreate)
				if err != nil {
					return false
				}
				return CRR.Name == fmt.Sprintf("%s-%s", oj.Name, podToRecreate.Name)
			}, 30*time.Second, 3*time.Second).ShouldNot(BeTrue())

			By("Check restartCount of pod")
			pods, err = clsTester.ListPodsForCollaSet(cls)
			Expect(err).NotTo(HaveOccurred())
			Expect(pods[0].Name).To(Equal(podToRecreate.Name))
			Expect(int(pods[0].Status.ContainerStatuses[0].RestartCount)).To(Equal(1))

			By("Wait for recreate OperationJob deleted")
			Eventually(func() bool {
				return errors.IsNotFound(ojTester.GetOperationJob(oj))
			}, 20*time.Second, 3*time.Second).Should(BeTrue())

			By("Check crr is cleaned")
			Eventually(func() bool {
				_, err := ojTester.GetForOperationJobTester(oj, podToRecreate)
				return errors.IsNotFound(err)
			}, 30*time.Second, 3*time.Second).Should(BeTrue())
		})
	})
})
