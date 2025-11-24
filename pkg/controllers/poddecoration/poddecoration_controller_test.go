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

package poddecoration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"kusionstack.io/kuperator/pkg/controllers/collaset"
	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/legacy"
	utilspoddecoration "kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/anno"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/strategy"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/utils/inject"
)

var (
	env    *envtest.Environment
	mgr    manager.Manager
	ctx    context.Context
	cancel context.CancelFunc
	c      client.Client
)

const (
	timeoutInterval = 10 * time.Second
	pollInterval    = 500 * time.Millisecond
)

var _ = Describe("PodDecoration controller", func() {
	It("test inject PodDecoration", func() {
		testcase := "test-pd-0"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		collaSetA := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"zone": "a",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"zone": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		// Create collaSet
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			return len(podList.Items)
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		podDecoration := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Weight: int32Pointer(1),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"zone": "a",
							},
						},
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar",
								Image: "nginx:v2",
							},
						},
					},
				},
			},
		}
		// Create pd
		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, timeoutInterval, pollInterval).Should(BeNil())
		// Wait for pd reconcile
		Eventually(func() int32 {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.Status.MatchedPods
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(int32(2)))
		// Expect two pods during ops
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
				}
			}
			return cnt
		}, 10*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
		// Allow Pod to update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}
		// Two pods recreated
		Eventually(func() int32 {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.Status.UpdatedPods
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(int32(2)))
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
		// Sidecar injected
		for _, po := range podList.Items {
			Expect(len(po.Spec.Containers)).Should(Equal(2))
		}
	})

	It("test reconcile multi CollaSet with one PodDecoration", func() {
		testcase := "test-pd-1"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		collaSetA := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"zone": "a",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"zone": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		collaSetB := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-b",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"zone": "b",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"zone": "b",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		podDecoration := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Weight: int32Pointer(10),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								appsv1alpha1.PodInstanceIDLabelKey: "0",
							},
						},
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar",
								Image: "nginx:v2",
							},
						},
					},
				},
			},
		}

		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() bool {
			err := c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
			return err == nil && podDecoration.Status.UpdatedRevision != "" &&
				podDecoration.Status.ObservedGeneration == podDecoration.Generation
		}, timeoutInterval, pollInterval).Should(BeTrue())
		Expect(podDecoration.Status.UpdatedRevision).Should(Equal(podDecoration.Status.CurrentRevision))
		// check effective PodDecoration in strategy manager
		Eventually(func() int {
			return len(strategy.SharedStrategyController.LatestPodDecorations(testcase))
		}, timeoutInterval, pollInterval).Should(Equal(1))
		// create CollaSet after podDecoration, do not need to allow Pod to update
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		Expect(c.Create(ctx, collaSetB)).Should(BeNil())
		podList := &corev1.PodList{}
		// wait all pods created
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				if len(po.Spec.Containers) == 2 {
					updatedCnt++
				}
			}
			return updatedCnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(4))

		// update PodDecoration
		Eventually(func() error {
			err := c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
			if err != nil {
				return err
			}
			podDecoration.Spec.Template.InitContainers = []*corev1.Container{
				{
					Name:  "init",
					Image: "nginx:v3",
				},
			}
			return c.Update(ctx, podDecoration)
		}, timeoutInterval, pollInterval).Should(BeNil())

		// Expect two pods during ops
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		// Allow Pod to update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			if pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] != "0" {
				continue
			}
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}
		// Two pods inject init container
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for _, po := range podList.Items {
				if len(po.Spec.InitContainers) == 1 {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
	})

	It("test delete PodDecoration", func() {
		testcase := "test-pd-2"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		collaSetA := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"zone": "a",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"zone": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		podDecoration := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Weight: int32Pointer(10),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{},
						},
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar",
								Image: "nginx:v2",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() int {
			return len(strategy.SharedStrategyController.LatestPodDecorations(testcase))
		}, timeoutInterval, pollInterval).Should(Equal(1))

		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			cnt := 0
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			for _, po := range podList.Items {
				if len(po.Spec.Containers) == 2 {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		// delete PodDecoration
		Expect(c.Delete(ctx, podDecoration)).Should(BeNil())
		// 2 pods during ops
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		Expect(c.List(ctx, podList, client.HasLabels{appsv1alpha1.PodDecorationLabelPrefix + podDecoration.Name})).ShouldNot(HaveOccurred())
		Expect(len(podList.Items)).Should(Equal(2))
		// allow Pod to do update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}
		// 2 pods escaped
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				if len(po.Spec.Containers) == 1 {
					updatedCnt++
				}
			}
			return updatedCnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		// annotation cleared
		for _, po := range podList.Items {
			Expect(po.Annotations[appsv1alpha1.AnnotationPodDecorationRevision]).Should(BeEquivalentTo("[]"))
			Expect(po.Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name]).Should(Equal(""))
		}
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, timeoutInterval, pollInterval).Should(HaveOccurred())
	})

	It("test PodDecoration weight", func() {
		testcase := "test-pd-3"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		collaSetA := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(3),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"zone": "a",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"zone": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		podDecorationA := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Weight: int32Pointer(10),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "collaset.kusionstack.io/instance-id",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"0", "1"},
								},
							},
						},
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar-1",
								Image: "nginx:v2",
							},
						},
					},
				},
			},
		}
		podDecorationB := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-b",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Weight: int32Pointer(11),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Selector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "collaset.kusionstack.io/instance-id",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"0", "1"},
								},
							},
						},
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar-2",
								Image: "nginx:v3",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			cnt := 0
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			for _, po := range podList.Items {
				if len(po.Spec.Containers) == 1 {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(3))

		Expect(c.Create(ctx, podDecorationA)).Should(BeNil())
		Eventually(func() int {
			return len(strategy.SharedStrategyController.LatestPodDecorations(testcase))
		}, timeoutInterval, pollInterval).Should(Equal(1))

		// 2 pods during ops
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))

		// allow Pod to do update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pod) {
				continue
			}
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}

		// 2 pods injected by PodDecoration-A
		Eventually(func() int {
			cnt := 0
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			for _, po := range podList.Items {
				if len(po.Spec.Containers) == 2 {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecorationA.Name, Namespace: testcase}, podDecorationA); err == nil {
				return podDecorationA.Status.CurrentRevision != podDecorationA.Status.UpdatedRevision
			}
			return false
		}, timeoutInterval, pollInterval).Should(BeTrue())

		// create PodDecoration-B
		Expect(c.Create(ctx, podDecorationB)).Should(BeNil())
		// 2 pods during ops
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		// allow Pod to do update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pod) {
				continue
			}
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}
		// 2 pods updated by PodDecoration-B
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecorationB.Name, Namespace: podDecorationB.Namespace}, podDecorationB)).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				info := utilspoddecoration.GetDecorationRevisionInfo(&po)
				if info.Size() == 2 && info.GetRevision(podDecorationB.Name) != nil &&
					*info.GetRevision(podDecorationB.Name) == podDecorationB.Status.UpdatedRevision {
					updatedCnt++
				}
			}
			return updatedCnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		Expect(c.List(ctx, podList, client.HasLabels{appsv1alpha1.PodDecorationLabelPrefix + podDecorationA.Name})).Should(BeNil())
		Expect(len(podList.Items)).Should(Equal(2))
		Expect(c.List(ctx, podList, client.HasLabels{appsv1alpha1.PodDecorationLabelPrefix + podDecorationB.Name})).Should(BeNil())
		Expect(len(podList.Items)).Should(Equal(2))
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				if len(po.Spec.Containers) != 3 {
					continue
				}
				if po.Spec.Containers[2].Name == "sidecar-2" {
					updatedCnt++
				}
			}
			return updatedCnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
	})

	It("test upgrade PodDecoration by partition", func() {
		testcase := "test-pd-4"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		collaSet := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(3),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"zone": "a",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"zone": "a",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "foo",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		podDecoration := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-a",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Weight: int32Pointer(10),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Partition: int32Pointer(2),
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar-1",
								Image: "nginx:v2",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, collaSet)).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			return len(podList.Items)
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(3))

		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() int {
			return len(strategy.SharedStrategyController.LatestPodDecorations(testcase))
		}, timeoutInterval, pollInterval).Should(Equal(1))
		instanceIds := sets.NewString()
		// 1 pod during ops
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i, po := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
					instanceIds.Insert(po.Labels[appsv1alpha1.PodInstanceIDLabelKey])
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(1))
		// 1 pods during ops
		Expect(instanceIds.Len()).Should(Equal(1))
		// allow Pod to do update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pod) {
				continue
			}
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}
		// 1 pod updated
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.HasLabels{appsv1alpha1.PodDecorationLabelPrefix + podDecoration.Name})).ShouldNot(HaveOccurred())
			return len(podList.Items)
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(1))

		Eventually(func() error {
			Expect(c.Get(ctx, types.NamespacedName{Namespace: testcase, Name: podDecoration.Name}, podDecoration)).Should(BeNil())
			podDecoration.Spec.UpdateStrategy.RollingUpdate.Partition = int32Pointer(1)
			return c.Update(ctx, podDecoration)
		}, timeoutInterval, pollInterval).Should(BeNil())
		// 1 pod during ops
		instanceIds = sets.NewString()
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
			cnt := 0
			for i, po := range podList.Items {
				if podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &podList.Items[i]) {
					cnt++
					instanceIds.Insert(po.Labels[appsv1alpha1.PodInstanceIDLabelKey])
				}
			}
			return cnt
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(1))
		// 1 pod during ops
		Expect(instanceIds.Len()).Should(Equal(1))
		// allow Pod to do update
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).ShouldNot(HaveOccurred())
		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to do update
			if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pod) {
				continue
			}
			Expect(updatePodWithRetry(ctx, c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}
		// 2 pod updated
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.HasLabels{appsv1alpha1.PodDecorationLabelPrefix + podDecoration.Name})).ShouldNot(HaveOccurred())
			return len(podList.Items)
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))
		Expect(podList.Items[0].Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name] == podList.Items[1].Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name]).Should(BeTrue())
	})
})

func TestPodDecorationController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodDecorationController Test Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}

	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())

	sch := scheme.Scheme
	Expect(appsv1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	Expect(appsv1alpha1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	mgr, err = manager.New(config, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           inject.NewCacheWithFieldIndex,
	})
	Expect(err).NotTo(HaveOccurred())
	c = mgr.GetClient()
	Expect(Add(mgr)).NotTo(HaveOccurred())
	Expect(collaset.Add(mgr)).NotTo(HaveOccurred())

	go func() {
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	cancel()

	err := env.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterEach(func() {
	csList := &appsv1alpha1.CollaSetList{}
	Expect(mgr.GetClient().List(context.Background(), csList)).Should(BeNil())

	for i := range csList.Items {
		Expect(mgr.GetClient().Delete(context.TODO(), &csList.Items[i])).Should(BeNil())
	}
	pods := &corev1.PodList{}
	Expect(mgr.GetClient().List(context.Background(), pods)).Should(BeNil())
	for i := range pods.Items {
		Expect(mgr.GetClient().Delete(context.TODO(), &pods.Items[i])).Should(BeNil())
	}
	Eventually(func() int {
		Expect(mgr.GetClient().List(context.Background(), pods)).Should(BeNil())
		return len(pods.Items)
	}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(0))

	pdList := &appsv1alpha1.PodDecorationList{}
	Expect(mgr.GetClient().List(context.Background(), pdList)).Should(BeNil())

	for i := range pdList.Items {
		Expect(mgr.GetClient().Delete(context.TODO(), &pdList.Items[i])).Should(BeNil())
	}
	Eventually(func() int {
		Expect(mgr.GetClient().List(context.Background(), pdList)).Should(BeNil())
		return len(pdList.Items)
	}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(0))
	nsList := &corev1.NamespaceList{}
	Expect(mgr.GetClient().List(context.Background(), nsList)).Should(BeNil())

	for i := range nsList.Items {
		if strings.HasPrefix(nsList.Items[i].Name, "test-") {
			mgr.GetClient().Delete(context.TODO(), &nsList.Items[i])
		}
	}
})

func createNamespace(c client.Client, namespaceName string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	return c.Create(context.TODO(), ns)
}

func int32Pointer(val int32) *int32 {
	return &val
}

func updatePodWithRetry(ctx context.Context, c client.Client, namespace, name string, updateFn func(pod *corev1.Pod) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			return err
		}

		if !updateFn(pod) {
			return nil
		}

		return c.Update(ctx, pod)
	})
}
