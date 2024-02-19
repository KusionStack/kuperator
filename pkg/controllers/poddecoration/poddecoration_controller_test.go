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
	"encoding/json"
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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/utils/inject"
)

var (
	env    *envtest.Environment
	mgr    manager.Manager
	ctx    context.Context
	cancel context.CancelFunc
	c      client.Client
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
		// 1, create collaSet
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-a",
					Weight: int32Pointer(10),
				},
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
		// 2, create pd
		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		Eventually(func() int32 {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.Status.MatchedPods
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(int32(2)))

		Eventually(func() bool {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.Status.IsEffective != nil && *podDecoration.Status.IsEffective
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(true))
		// 3, Expect two pods during ops
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
		// 4, Allow Pod to update
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
		// 5, Two pods recreated
		Eventually(func() int32 {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.Status.UpdatedPods
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(int32(2)))
		Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Weight: int32Pointer(10),
				},
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
			if err := c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration); err == nil {
				if podDecoration.Status.IsEffective != nil {
					return *podDecoration.Status.IsEffective
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(podDecoration.Status.UpdatedRevision).ShouldNot(Equal(""))
		Expect(podDecoration.Status.UpdatedRevision).Should(Equal(podDecoration.Status.CurrentRevision))
		// create CollaSet after podDecoration, do not need to allow Pod to update
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		Expect(c.Create(ctx, collaSetB)).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				if len(po.Spec.Containers) == 2 {
					updatedCnt++
				}
			}
			return updatedCnt
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(4))

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
		}, 5*time.Second, 1*time.Second).Should(BeNil())

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
		}, 10*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Weight: int32Pointer(10),
				},
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
		Eventually(func() bool {
			if c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration) == nil {
				return len(podDecoration.Finalizers) != 0 && podDecoration.Status.IsEffective != nil && *podDecoration.Status.IsEffective
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(true))

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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
		Expect(c.Delete(ctx, podDecoration)).Should(BeNil())
		// PodDecoration is disabled
		Eventually(func() interface{} {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.DeletionTimestamp
		}, 5*time.Second, 1*time.Second).ShouldNot(BeNil())

		getter, err := collasetutils.NewPodDecorationGetter(ctx, c, testcase)
		Expect(err).Should(BeNil())
		Expect(len(getter.GetLatestDecorations())).Should(Equal(0))

		Eventually(func() bool {
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)).Should(BeNil())
			return podDecoration.Status.IsEffective != nil && *podDecoration.Status.IsEffective
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(true))
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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
		// annotation cleared
		for _, po := range podList.Items {
			Expect(po.Annotations[appsv1alpha1.AnnotationPodDecorationRevision]).Should(BeEquivalentTo("[]"))
		}
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, 5*time.Second, 1*time.Second).Should(HaveOccurred())
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-a",
					Weight: int32Pointer(10),
				},
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-a",
					Weight: int32Pointer(11),
				},
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
								Name:  "sidecar-2",
								Image: "nginx:v3",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, podDecorationA)).Should(BeNil())
		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecorationA.Name, Namespace: testcase}, podDecorationA); err == nil {
				if podDecorationA.Status.IsEffective != nil {
					return *podDecorationA.Status.IsEffective
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		podList := &corev1.PodList{}
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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))

		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecorationA.Name, Namespace: testcase}, podDecorationA); err == nil {
				return podDecorationA.Status.CurrentRevision == podDecorationA.Status.UpdatedRevision
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
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
		// 2 pods updated by PodDecoration-B
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecorationB.Name, Namespace: podDecorationB.Namespace}, podDecorationB)).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				info := utilspoddecoration.GetDecorationRevisionInfo(&po)
				if info.Size() == 1 && info.GetRevision(podDecorationB.Name) != nil && *info.GetRevision(podDecorationB.Name) == podDecorationB.Status.UpdatedRevision {
					updatedCnt++
				}
			}
			return updatedCnt
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
	})

	It("test no group PodDecoration", func() {
		testcase := "test-pd-4"
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-a",
					Weight: int32Pointer(10),
				},
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
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Weight: int32Pointer(10),
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
		podDecorationC := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo-c",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				HistoryLimit: 5,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Weight: int32Pointer(11),
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar-3",
								Image: "nginx:v4",
							},
						},
					},
				},
			},
		}
		Expect(c.Create(ctx, podDecorationA)).Should(BeNil())
		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecorationA.Name, Namespace: testcase}, podDecorationA); err == nil {
				if podDecorationA.Status.IsEffective != nil {
					return *podDecorationA.Status.IsEffective
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Create(ctx, collaSetA)).Should(BeNil())
		podList := &corev1.PodList{}
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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))

		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecorationA.Name, Namespace: testcase}, podDecorationA); err == nil {
				return podDecorationA.Status.CurrentRevision == podDecorationA.Status.UpdatedRevision
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
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
		// 2 pods inject PodDecoration-B
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecorationB.Name, Namespace: podDecorationB.Namespace}, podDecorationB)).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				info := utilspoddecoration.GetDecorationRevisionInfo(&po)
				if info.Size() == 2 &&
					info.GetRevision(podDecorationB.Name) != nil &&
					*info.GetRevision(podDecorationB.Name) == podDecorationB.Status.UpdatedRevision &&
					info.GetRevision(podDecorationA.Name) != nil &&
					*info.GetRevision(podDecorationA.Name) == podDecorationA.Status.UpdatedRevision {
					updatedCnt++
				}
			}
			return updatedCnt
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))

		// create PodDecoration-C
		Expect(c.Create(ctx, podDecorationC)).Should(BeNil())
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
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
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
		// 2 pods inject PodDecoration-C
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			Expect(c.Get(ctx, types.NamespacedName{Name: podDecorationC.Name, Namespace: podDecorationC.Namespace}, podDecorationC)).Should(BeNil())
			updatedCnt := 0
			for _, po := range podList.Items {
				info := utilspoddecoration.GetDecorationRevisionInfo(&po)
				if info.Size() == 3 {
					updatedCnt++
				}
			}
			return updatedCnt
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
		Expect(len(podList.Items[0].Spec.Containers)).Should(BeEquivalentTo(4))
		// B -> C -> A
		// foo -> sidecar-3 -> sidecar-2 -> sidecar-1
		Expect(podList.Items[0].Spec.Containers[0].Name).Should(BeEquivalentTo("foo"))
		Expect(podList.Items[0].Spec.Containers[1].Name).Should(BeEquivalentTo("sidecar-3"))
		Expect(podList.Items[0].Spec.Containers[2].Name).Should(BeEquivalentTo("sidecar-2"))
		Expect(podList.Items[0].Spec.Containers[3].Name).Should(BeEquivalentTo("sidecar-1"))
	})
})

func TestPodDecorationController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CollaSetController Test Suite")
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

func printJson(obj any) {
	byt, _ := json.MarshalIndent(obj, "", "  ")
	fmt.Printf("%s\n", string(byt))
}
