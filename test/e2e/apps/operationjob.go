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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/rand"
	clientset "k8s.io/client-go/kubernetes"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/test/e2e/framework"
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
					Name: podToReplace.Name,
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
					Name: pods[0].Name,
				}, {
					Name: pods[1].Name,
				}, {
					Name: pods[2].Name,
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
					Name: "non-exist-pod",
				},
			})
			Expect(ojTester.CreateOperationJob(oj)).NotTo(HaveOccurred())

			By("Wait for replace OperationJob Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj, appsv1alpha1.OperationProgressFailed) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check reason for Failed")
			Expect(oj.Status.TargetDetails[0].Reason).To(Equal(appsv1alpha1.ReasonPodNotFound))
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
					Name: podToReplace.Name,
				},
			})
			oj2 := ojTester.NewOperationJob("operationjob-"+randStr+"-2", appsv1alpha1.OpsActionReplace, []appsv1alpha1.PodOpsTarget{
				{
					Name: podToReplace.Name,
				},
			})
			Expect(ojTester.CreateOperationJob(oj1)).NotTo(HaveOccurred())
			Expect(ojTester.CreateOperationJob(oj2)).NotTo(HaveOccurred())

			By("Wait for OperationJobs Succeeded")
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj1, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())
			Eventually(func() error { return ojTester.ExpectOperationJobProgress(oj2, appsv1alpha1.OperationProgressSucceeded) }, 30*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

			By("Check replace new pod of OperationJobs")
			Expect(oj1.Status.TargetDetails[0].Message).To(Equal(oj2.Status.TargetDetails[0].Message))
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
					Name: podToReplace.Name,
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
					Name: podToReplace.Name,
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

})
