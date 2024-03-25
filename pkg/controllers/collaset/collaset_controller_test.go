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

package collaset

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/synccontrol"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	"kusionstack.io/operating/pkg/controllers/poddeletion"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/utils/inject"
)

var (
	env     *envtest.Environment
	mgr     manager.Manager
	request chan reconcile.Request

	ctx    context.Context
	cancel context.CancelFunc
	c      client.Client
)

var _ = Describe("collaset controller", func() {

	It("scale reconcile", func() {
		testcase := "test-scale"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
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
							},
						},
					},
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())

		// pod replica will be kept, after deleted
		podToDelete := &podList.Items[0]
		// add finalizer to block its deletion
		Expect(updatePodWithRetry(c, podToDelete.Namespace, podToDelete.Name, func(pod *corev1.Pod) bool {
			pod.Finalizers = append(pod.Finalizers, "block/deletion")
			return true
		})).Should(BeNil())
		Expect(c.Delete(context.TODO(), podToDelete)).Should(BeNil())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: podToDelete.Namespace, Name: podToDelete.Name}, podToDelete)).Should(BeNil())
			return podToDelete.DeletionTimestamp != nil
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// there should be 3 pods and one of them is terminating
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 3
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(len(podList.Items)).Should(BeEquivalentTo(3))
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())
		// should use Pod instance ID: 0, 1
		podInstanceID := sets.Int{}
		podNames := sets.String{}
		for _, pod := range podList.Items {
			id, err := collasetutils.GetPodInstanceID(&pod)
			Expect(err).Should(BeNil())
			podInstanceID.Insert(id)
			podNames.Insert(pod.Name)
		}
		Expect(podInstanceID.Len()).Should(BeEquivalentTo(2))
		Expect(podInstanceID.Has(0)).Should(BeTrue())
		Expect(podInstanceID.Has(1)).Should(BeTrue())
		Expect(podNames.Has(podToDelete.Name)).Should(BeTrue())
		// let terminating Pod disappeared
		Expect(updatePodWithRetry(c, podToDelete.Namespace, podToDelete.Name, func(pod *corev1.Pod) bool {
			pod.Finalizers = []string{}
			return true
		})).Should(BeNil())
		Eventually(func() bool {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: podToDelete.Namespace, Name: podToDelete.Name}, podToDelete) != nil
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// scale in pods with delay seconds
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
			return true
		})).Should(BeNil())

		// mark all pods allowed to operate in PodOpsLifecycle
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Expect(len(podList.Items)).Should(BeEquivalentTo(2))
		for i := range podList.Items {
			pod := &podList.Items[i]
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}

		Eventually(func() error {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			if len(podList.Items) != 1 {
				return fmt.Errorf("expected 1 pods, got %d", len(podList.Items))
			}

			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// scale in pods with delay seconds
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(0)
			cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
			return true
		}))

		Eventually(func() error {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			if len(podList.Items) != 0 {
				return fmt.Errorf("expected 0 pods, got %d", len(podList.Items))
			}

			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return expectedStatusReplicas(c, cs, 0, 0, 0, 0, 0, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())
	})

	It("update reconcile", func() {
		testcase := "test-update"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
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
							},
						},
					},
				},
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)).Should(BeNil())

		// test update ByPartition
		for _, partition := range []int32{0, 1, 2, 3, 4} {
			observedGeneration := cs.Status.ObservedGeneration

			// update CollaSet image and partition
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: &partition,
					},
				}
				cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"

				return true
			})).Should(BeNil())
			Eventually(func() bool {
				Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
				return cs.Status.ObservedGeneration != observedGeneration
			}, 5*time.Second, 1*time.Second).Should(BeTrue())

			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := &podList.Items[i]
				if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pod) {
					continue
				}

				if _, allowed := podopslifecycle.AllowOps(collasetutils.UpdateOpsLifecycleAdapter, 0, pod); allowed {
					continue
				}
				// allow Pod to do update
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
			// check updated pod replicas by CollaSet status
			Eventually(func() error {
				return expectedStatusReplicas(c, cs, 0, 0, 0, 4, partition, partition, 0, 0)
			}, 5*time.Second, 1*time.Second).Should(BeNil())

			// double check updated pod replicas
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			updatedReplicas := 0
			for _, pod := range podList.Items {
				if pod.Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
					updatedReplicas++
					Expect(pod.Annotations).ShouldNot(BeNil())
					Expect(pod.Annotations[appsv1alpha1.LastPodStatusAnnotationKey]).ShouldNot(BeEquivalentTo(""))

					podStatus := &synccontrol.PodStatus{}
					Expect(json.Unmarshal([]byte(pod.Annotations[appsv1alpha1.LastPodStatusAnnotationKey]), podStatus)).Should(BeNil())
					Expect(len(podStatus.ContainerStates)).Should(BeEquivalentTo(1))
					Expect(podStatus.ContainerStates["foo"].LatestImage).Should(BeEquivalentTo("nginx:v2"))
				}
			}
			Expect(updatedReplicas).Should(BeEquivalentTo(partition))
		}

		// mock Pods updated by kubelet
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			Expect(updatePodStatusWithRetry(c, podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{
						Name:    "foo",
						ImageID: "id:v2",
					},
				}
				return true
			})).Should(BeNil())
		}

		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// test update ByLabel
		observedGeneration := cs.Status.ObservedGeneration
		// update CollaSet image
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByLabel: &appsv1alpha1.ByLabel{},
			}
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v3"

			return true
		})).Should(BeNil())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.ObservedGeneration != observedGeneration
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for _, number := range []int32{1, 2, 3, 4} {
			pod := podList.Items[number-1]
			// label pod to trigger update
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				if pod.Labels == nil {
					pod.Labels = map[string]string{}
				}
				pod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] = "true"
				return true
			})).Should(BeNil())

			Eventually(func() error {
				// check updated pod replicas by CollaSet status
				return expectedStatusReplicas(c, cs, 0, 0, 0, 4, number, 1, 0, 0)
			}, 5*time.Second, 1*time.Second).Should(BeNil())

			Expect(updatePodStatusWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{
						Name:    "foo",
						ImageID: "id:v3",
					},
				}
				return true
			})).Should(BeNil())

			// double check updated pod replicas
			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			updatedReplicas := 0
			for _, pod := range podList.Items {
				Eventually(func() bool {
					tmpPod := &corev1.Pod{}
					Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, tmpPod)).Should(BeNil())
					return tmpPod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] == ""
				}, 5*time.Second, 1*time.Second).Should(BeTrue())
				if pod.Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
					updatedReplicas++
				}
			}
			Expect(updatedReplicas).Should(BeEquivalentTo(number))

			Eventually(func() error {
				// check updated pod replicas by CollaSet status
				return expectedStatusReplicas(c, cs, 0, 0, 0, 4, number, 0, 0, 0)
			}, 5*time.Second, 1*time.Second).Should(BeNil())
		}
	})

	It("update pod policy", func() {
		testcase := "test-update-pod-policy"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":   "foo",
							"test1": "v1",
							"test2": "v1",
							"test3": "v1",
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

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)).Should(BeNil())

		pod := podList.Items[0]
		// test attribute updated manually
		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels["test1"] = "v2"
			pod.Labels["manual-added"] = "v1"
			delete(pod.Labels, "test2")
			return true
		})).Should(BeNil())

		observedGeneration := cs.Status.ObservedGeneration
		// update CollaSet image
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Template.Labels["test1"] = "v3"
			delete(cls.Spec.Template.Labels, "test3")
			return true
		})).Should(BeNil())

		// allow Pod to update
		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.ObservedGeneration != observedGeneration && cs.Status.UpdatedReplicas == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// check pod label
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &pod)).Should(BeNil())
		Expect(pod.Labels).ShouldNot(BeNil())
		// manual changes should have lower priority than PodTemplate changes
		Expect(pod.Labels["test1"]).Should(BeEquivalentTo("v3"))
		Expect(pod.Labels["manual-added"]).Should(BeEquivalentTo("v1"))

		_, exist := pod.Labels["test2"]
		Expect(exist).Should(BeFalse())
		_, exist = pod.Labels["test3"]
		Expect(exist).Should(BeFalse())

		// test Recreate policy
		observedGeneration = cs.Status.ObservedGeneration
		// update CollaSet image
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Template.Labels["test1"] = "v4"
			cls.Spec.UpdateStrategy.PodUpdatePolicy = appsv1alpha1.CollaSetRecreatePodUpdateStrategyType
			return true
		})).Should(BeNil())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.ObservedGeneration != observedGeneration && cs.Status.UpdatedReplicas == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// pod should be recreated
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &pod)).ShouldNot(BeNil())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Expect(len(podList.Items)).Should(BeEquivalentTo(1))
		pod = podList.Items[0]
		// check pod label
		Expect(pod.Labels).ShouldNot(BeNil())
		// manual changes should have lower priority than PodTemplate changes
		Expect(pod.Labels["test1"]).Should(BeEquivalentTo("v4"))
		Expect(pod.Labels["test2"]).Should(BeEquivalentTo("v1"))

		_, exist = pod.Labels["manual-added"]
		Expect(exist).Should(BeFalse())
		_, exist = pod.Labels["test3"]
		Expect(exist).Should(BeFalse())
	})

	It("pod recreate with its current revision", func() {
		testcase := "test-pod-recreate-with-its-revision"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
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
							},
						},
					},
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())

		var partition int32 = 1
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByPartition: &appsv1alpha1.ByPartition{
					Partition: &partition,
				},
			}
			return true
		}))

		for i := range podList.Items {
			pod := &podList.Items[i]
			// allow Pod to update
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = "true"
				return true
			})).Should(BeNil())
		}
		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 1, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		idToRevision := map[int]string{}
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Expect(len(podList.Items)).Should(BeEquivalentTo(2))
		for i := range podList.Items {
			pod := &podList.Items[i]
			id, _ := collasetutils.GetPodInstanceID(pod)
			Expect(id >= 0).Should(BeTrue())
			revision := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
			Expect(revision).ShouldNot(BeEquivalentTo(""))
			idToRevision[id] = revision
		}
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())

		// delete pod
		for i := range podList.Items {
			Expect(c.Delete(context.TODO(), &podList.Items[i])).Should(BeNil())
		}

		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())
		for i := range podList.Items {
			pod := &podList.Items[i]
			id, _ := collasetutils.GetPodInstanceID(pod)
			Expect(id >= 0).Should(BeTrue())
			revision := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
			Expect(revision).ShouldNot(BeEquivalentTo(""))
			Expect(idToRevision[id]).Should(BeEquivalentTo(revision))
		}
	})

	It("replace update reconcile by partition", func() {
		testcase := "test-replace-update-by-partition"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
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
							},
						},
					},
				},
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType,
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)).Should(BeNil())

		// test update ByPartition
		for _, partition := range []int32{0, 1, 2, 3, 4} {
			observedGeneration := cs.Status.ObservedGeneration

			// update CollaSet image and partition
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: &partition,
					},
				}
				cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"

				return true
			})).Should(BeNil())
			Eventually(func() bool {
				Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
				return cs.Status.ObservedGeneration != observedGeneration
			}, 5*time.Second, 1*time.Second).Should(BeTrue())

			// check updated pod replicas by CollaSet status
			Eventually(func() error {
				return expectedStatusReplicas(c, cs, 0, 0, 0, 4+partition, partition, 0, 0, 0)
			}, 30*time.Second, 1*time.Second).Should(BeNil())

			// double check updated pod replicas
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			updatedReplicas := 0
			for _, pod := range podList.Items {
				if pod.Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
					updatedReplicas++
					Expect(pod.Labels).ShouldNot(BeNil())
					Expect(pod.Labels[appsv1.ControllerRevisionHashLabelKey]).Should(BeEquivalentTo(cs.Status.UpdatedRevision))
				}
			}
			Expect(updatedReplicas).Should(BeEquivalentTo(partition))
		}

		// mock Pods updated by kubelet
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if podList.Items[i].Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
				Expect(updatePodStatusWithRetry(c, podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Status.ContainerStatuses = []corev1.ContainerStatus{
						{
							Name:    "foo",
							ImageID: "id:v2",
						},
					}
					return true
				})).Should(BeNil())
			}
		}

		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 8, 4, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// mock Pods updated serviceAvailable
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if _, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
				Expect(updatePodWithRetry(c, podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
			}
		}

		// wait and check new pod service available
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 4, 8, 4, 0, 0, 4)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		// check all origin pod label with to-delete
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		inDeleteReplicas := 0
		for _, pod := range podList.Items {
			if _, exist := pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; exist {
				inDeleteReplicas++
				Expect(pod.Labels[appsv1alpha1.PodReplacePairNewId]).ShouldNot(BeNil())
				Expect(pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]).ShouldNot(BeNil())
			}
		}
		Expect(inDeleteReplicas).Should(BeEquivalentTo(4))
	})

	It("replace update reconcile by label", func() {
		testcase := "test-replace-update-by-label"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
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
							},
						},
					},
				},
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType,
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)).Should(BeNil())

		// test update ByLabel
		observedGeneration := cs.Status.ObservedGeneration
		// update CollaSet image
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByLabel: &appsv1alpha1.ByLabel{},
			}
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"

			return true
		})).Should(BeNil())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.ObservedGeneration != observedGeneration
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())

		originPodList := podList.DeepCopy()
		for index, pod := range originPodList.Items {
			// label pod to trigger update
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				if pod.Labels == nil {
					pod.Labels = map[string]string{}
				}
				pod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] = "true"
				return true
			})).Should(BeNil())

			Eventually(func() error {
				// check updated pod replicas by CollaSet status
				return expectedStatusReplicas(c, cs, 0, 0, 0, int32(4+index+1), int32(index+1), 0, 0, 0)
			}, 30*time.Second, 1*time.Second).Should(BeNil())

			Expect(updatePodStatusWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				pod.Status.ContainerStatuses = []corev1.ContainerStatus{
					{
						Name:    "foo",
						ImageID: "id:v2",
					},
				}
				return true
			})).Should(BeNil())

			// double check updated pod replicas
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			updatedReplicas := 0
			for _, pod := range podList.Items {
				if pod.Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
					updatedReplicas++
					Expect(pod.Labels).ShouldNot(BeNil())
					Expect(pod.Labels[appsv1.ControllerRevisionHashLabelKey]).Should(BeEquivalentTo(cs.Status.UpdatedRevision))
				}
			}
			Expect(updatedReplicas).Should(BeEquivalentTo(index + 1))
		}

		// mock Pods updated by kubelet
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if podList.Items[i].Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
				Expect(updatePodStatusWithRetry(c, podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Status.ContainerStatuses = []corev1.ContainerStatus{
						{
							Name:    "foo",
							ImageID: "id:v2",
						},
					}
					return true
				})).Should(BeNil())
			}
		}

		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 8, 4, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// mock Pods updated serviceAvailable
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if _, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
				Expect(updatePodWithRetry(c, podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
			}
		}

		// wait and check new pod service available
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 4, 8, 4, 0, 0, 4)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		// check all origin pod label with to-delete
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		inDeleteReplicas := 0
		for _, pod := range podList.Items {
			if _, exist := pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; exist {
				inDeleteReplicas++
				Expect(pod.Labels[appsv1alpha1.PodReplacePairNewId]).ShouldNot(BeNil())
				Expect(pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]).ShouldNot(BeNil())
			}
		}
		Expect(inDeleteReplicas).Should(BeEquivalentTo(4))
	})

	It("replace pod by label", func() {
		testcase := "test-replace-pod-by-label"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(1),
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
							},
						},
					},
				},
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType,
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)).Should(BeNil())

		observedGeneration := cs.Status.ObservedGeneration
		// update CollaSet image
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByLabel: &appsv1alpha1.ByLabel{},
			}
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"

			return true
		})).Should(BeNil())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.ObservedGeneration != observedGeneration
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())

		replacePod := podList.Items[0]
		// label pod to trigger update
		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 0, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		// double check updated pod replicas
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		var replacePairNewId, newPodInstanceId string
		for _, pod := range podList.Items {
			Expect(pod.Labels).ShouldNot(BeNil())
			// replace by current revision
			Expect(pod.Labels[appsv1.ControllerRevisionHashLabelKey]).Should(BeEquivalentTo(cs.Status.CurrentRevision))
			Expect(pod.Spec.Containers[0].Image).Should(BeEquivalentTo("nginx:v1"))
			if pod.Name == replacePod.Name {
				replacePairNewId = pod.Labels[appsv1alpha1.PodReplacePairNewId]
				Expect(replacePairNewId).ShouldNot(BeNil())
			} else {
				newPodInstanceId = pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(pod.Labels[appsv1alpha1.PodReplacePairOriginName]).Should(BeEquivalentTo(replacePod.Name))
			}
		}
		Expect(replacePairNewId).ShouldNot(BeEquivalentTo(""))
		Expect(newPodInstanceId).ShouldNot(BeEquivalentTo(""))
		Expect(newPodInstanceId).Should(BeEquivalentTo(replacePairNewId))
	})

	It("replace update change to inplaceUpdate", func() {
		testcase := "test-replace-update-change-to-inplace-update"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(1),
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
							},
						},
					},
				},
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType,
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)).Should(BeNil())

		observedGeneration := cs.Status.ObservedGeneration
		// update CollaSet image
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByLabel: &appsv1alpha1.ByLabel{},
			}
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"

			return true
		})).Should(BeNil())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.ObservedGeneration != observedGeneration
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		originPod := podList.Items[0]

		// label pod to trigger update
		Expect(updatePodWithRetry(c, originPod.Namespace, originPod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.PodUpdatePolicy = appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType
			return true
		})).Should(BeNil())

		Eventually(func() bool {
			pod := &corev1.Pod{}
			error := c.Get(context.TODO(), types.NamespacedName{Namespace: originPod.Namespace, Name: originPod.Name}, pod)
			Expect(error).Should(BeNil())
			Expect(pod.Labels).ShouldNot(BeNil())
			_, replaceIndicate := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			_, replaceByUpdate := pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
			// check updated pod replicas by CollaSet status
			return !replaceIndicate && !replaceByUpdate
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		// double check updated pod replicas
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for _, pod := range podList.Items {
			if pod.Spec.Containers[0].Image == cs.Spec.Template.Spec.Containers[0].Image {
				// check new pod to delete
				Expect(pod.Labels).ShouldNot(BeNil())
				Expect(pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]).ShouldNot(BeNil())
				Expect(pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]).ShouldNot(BeNil())
			}
		}
	})

	It("pvc provision", func() {
		testcase := "pvc-provision"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
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
									{
										Name:      "pvc2",
										MountPath: "/tmp/pvc2",
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
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc2",
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
		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		// test1: pvc provision
		{
			podList := &corev1.PodList{}
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 4
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)).Should(BeNil())
			// there should be 8 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
			}
			Expect(len(activePvcs) == 8).Should(BeTrue())
			// check instance id consistency
			idBounded := sets.String{}
			for i := range podList.Items {
				id := podList.Items[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				idBounded.Insert(id)
			}
			for i := range activePvcs {
				id := activePvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(idBounded.Has(id)).Should(BeTrue())
			}
		}

		// test2: scale in pods with pvc template
		{
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.Replicas = int32Pointer(2)
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				return true
			})).Should(BeNil())
			// mark all pods allowed to operate in PodOpsLifecycle
			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			Expect(len(podList.Items)).Should(BeEquivalentTo(4))
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
			// there should be 2 pods
			podList = &corev1.PodList{}
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 2
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())
			// there should be 4 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
			}
			Expect(len(activePvcs) == 4).Should(BeTrue())
			// check instance id consistency
			idBounded := sets.String{}
			for i := range podList.Items {
				id := podList.Items[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				idBounded.Insert(id)
			}
			for i := range activePvcs {
				id := activePvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(idBounded.Has(id)).Should(BeTrue())
			}
			// complete scale in podOpsLifeCycle
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
					delete(pod.Labels, labelOperate)
					return true
				})).Should(BeNil())
			}
		}

		// test3: scale out pods with pvc template
		{
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.Replicas = int32Pointer(4)
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				return true
			})).Should(BeNil())
			podList := &corev1.PodList{}
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 4
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)).Should(BeNil())
			// there should be 8 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
			}
			Expect(len(activePvcs) == 8).Should(BeTrue())
			// check instance id consistency
			idBounded := sets.String{}
			for i := range podList.Items {
				id := podList.Items[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				idBounded.Insert(id)
			}
			for i := range activePvcs {
				id := activePvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(idBounded.Has(id)).Should(BeTrue())
			}
		}
	})

	It("pvc template update", func() {
		testcase := "pvc-template-update"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(3),
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
									{
										Name:      "pvc2",
										MountPath: "/tmp/pvc2",
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
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc2",
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
		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 3
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)).Should(BeNil())
		for _, partition := range []int32{0, 1, 2, 3, 4} {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc1",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"storage": resource.MustParse("200m"),
								},
							},
							AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc2",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"storage": resource.MustParse("300m"),
								},
							},
							AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
						},
					},
				}
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: &partition,
					},
				}
				cls.Spec.UpdateStrategy.PodUpdatePolicy = appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				return true
			})).Should(BeNil())
			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			// allow Pod to do update
			for i := range podList.Items {
				pod := &podList.Items[i]
				if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, pod) {
					continue
				}

				if _, allowed := podopslifecycle.AllowOps(collasetutils.UpdateOpsLifecycleAdapter, 0, pod); allowed {
					continue
				}
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
			// wait for recreate update finished
			time.Sleep(3 * time.Second)
			// there should be 6 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
			}
			Expect(len(activePvcs) == 6).Should(BeTrue())
			// check instance id consistency
			podList = &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			idBounded := sets.String{}
			for i := range podList.Items {
				id := podList.Items[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				idBounded.Insert(id)
			}
			for i := range activePvcs {
				id := activePvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(idBounded.Has(id)).Should(BeTrue())
			}
			// check storage
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			pvcHashMapping, _ := collasetutils.PvcTmpHashMapping(cs.Spec.VolumeClaimTemplates)
			for i := range activePvcs {
				id := activePvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(idBounded.Has(id)).Should(BeTrue())
				pvcTmpName, _ := collasetutils.ExtractPvcTmpName(cs, activePvcs[i])
				quantity := activePvcs[i].Spec.Resources.Requests["storage"]
				hash := activePvcs[i].Labels[appsv1alpha1.PvcTemplateHashLabelKey]
				if hash == pvcHashMapping[pvcTmpName] {
					if pvcTmpName == "pvc1" {
						Expect(quantity.String() == "200m").Should(BeTrue())
					} else if pvcTmpName == "pvc2" {
						Expect(quantity.String() == "300m").Should(BeTrue())
					}
				}
			}
		}

	})

	It("pvc with replace update", func() {
		testcase := "pvc-with-replace-update"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(3),
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
									{
										Name:      "pvc2",
										MountPath: "/tmp/pvc2",
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
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc2",
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
		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 3
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)).Should(BeNil())
		for _, partition := range []int32{0, 1, 2, 3} {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: &partition,
					},
				}
				cls.Spec.UpdateStrategy.PodUpdatePolicy = appsv1alpha1.CollaSetReplaceUpdatePodUpdateStrategyType
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
				return true
			})).Should(BeNil())
			// wait for reconcile finished
			time.Sleep(3 * time.Second)
			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			// allow Pod to do replace
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					if _, exist := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; exist {
						pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
					}
					return true
				})).Should(BeNil())
			}
			// wait for replace update finished
			time.Sleep(3 * time.Second)
			// there should be 6 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
			}
			Expect(len(activePvcs) == 6).Should(BeTrue())
			// check instance id consistency
			podList = &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			idBounded := sets.String{}
			for i := range podList.Items {
				id := podList.Items[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				idBounded.Insert(id)
			}
			for i := range activePvcs {
				id := activePvcs[i].Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(idBounded.Has(id)).Should(BeTrue())
			}
		}
	})

	It("pvc retention policy", func() {
		testcase := "pvc-retention-policy"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
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
									{
										Name:      "pvc2",
										MountPath: "/tmp/pvc2",
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
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc2",
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
		csReCreate := cs.DeepCopy()
		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		// test1: pvc whenScaled retention policy
		{
			podList := &corev1.PodList{}
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 4
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4, 0, 0, 0)).Should(BeNil())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			// scale in 2 pods
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.Replicas = int32Pointer(2)
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{
					WhenScaled: appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
				}
				return true
			})).Should(BeNil())
			// mark all pods allowed to operate in PodOpsLifecycle
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			Expect(len(podList.Items)).Should(BeEquivalentTo(4))
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
			// there should be 2 pods
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 2
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			// there should be 8 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			pvcNames := sets.String{}
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
				pvcNames.Insert(allPvcs.Items[i].Name)
			}
			Expect(len(activePvcs) == 8).Should(BeTrue())
			// complete scale in podOpsLifeCycle
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
					delete(pod.Labels, labelOperate)
					return true
				})).Should(BeNil())
			}
			// scale out 2 pods
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.Replicas = int32Pointer(4)
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				return true
			})).Should(BeNil())
			// there should be 4 pods
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 4
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			// pvcs should be retained
			for i := range podList.Items {
				pod := podList.Items[i]
				for j := range pod.Spec.Volumes {
					volume := pod.Spec.Volumes[j]
					Expect(pvcNames.Has(volume.PersistentVolumeClaim.ClaimName)).Should(BeTrue())
				}
			}
		}

		// test2: pvc whenDeleted retention policy
		{
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{
					WhenDeleted: appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
				}
				return true
			})).Should(BeNil())
			// delete set and pods
			Expect(c.Delete(context.TODO(), cs)).Should(BeNil())
			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(csReCreate.Namespace))).Should(BeNil())
			for _, pod := range podList.Items {
				Expect(c.Delete(context.TODO(), pod.DeepCopy())).Should(BeNil())
			}
			time.Sleep(3 * time.Second)
			// there should be 8 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
			pvcNames := sets.String{}
			for i := range allPvcs.Items {
				if allPvcs.Items[i].DeletionTimestamp != nil {
					continue
				}
				activePvcs = append(activePvcs, &allPvcs.Items[i])
				pvcNames.Insert(allPvcs.Items[i].Name)
			}
			// recreate set
			csReCreate.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
			}
			Expect(c.Create(context.TODO(), csReCreate)).Should(BeNil())
			time.Sleep(3 * time.Second)
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(csReCreate.Namespace))).Should(BeNil())
				return len(podList.Items) == 4
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: csReCreate.Namespace, Name: csReCreate.Name}, csReCreate)).Should(BeNil())
			// pvcs should be retained
			for i := range podList.Items {
				pod := podList.Items[i]
				for j := range pod.Spec.Volumes {
					volume := pod.Spec.Volumes[j]
					Expect(pvcNames.Has(volume.PersistentVolumeClaim.ClaimName)).Should(BeTrue())
				}
			}
		}
	})
})

func expectedStatusReplicas(c client.Client, cls *appsv1alpha1.CollaSet, scheduledReplicas, readyReplicas, availableReplicas, replicas, updatedReplicas, operatingReplicas,
	updatedReadyReplicas, updatedAvailableReplicas int32) error {
	if err := c.Get(context.TODO(), types.NamespacedName{Namespace: cls.Namespace, Name: cls.Name}, cls); err != nil {
		return err
	}

	if cls.Status.ScheduledReplicas != scheduledReplicas {
		return fmt.Errorf("scheduledReplicas got %d, expected %d", cls.Status.ScheduledReplicas, scheduledReplicas)
	}

	if cls.Status.ReadyReplicas != readyReplicas {
		return fmt.Errorf("readyReplicas got %d, expected %d", cls.Status.ReadyReplicas, readyReplicas)
	}

	if cls.Status.AvailableReplicas != availableReplicas {
		return fmt.Errorf("availableReplicas got %d, expected %d", cls.Status.AvailableReplicas, availableReplicas)
	}

	if cls.Status.Replicas != replicas {
		return fmt.Errorf("replicas got %d, expected %d", cls.Status.Replicas, replicas)
	}

	if cls.Status.UpdatedReplicas != updatedReplicas {
		return fmt.Errorf("updatedReplicas got %d, expected %d", cls.Status.UpdatedReplicas, updatedReplicas)
	}

	if cls.Status.OperatingReplicas != operatingReplicas {
		return fmt.Errorf("operatingReplicas got %d, expected %d", cls.Status.OperatingReplicas, operatingReplicas)
	}

	if cls.Status.UpdatedReadyReplicas != updatedReadyReplicas {
		return fmt.Errorf("updatedReadyReplicas got %d, expected %d", cls.Status.UpdatedReadyReplicas, updatedReadyReplicas)
	}

	if cls.Status.UpdatedAvailableReplicas != updatedAvailableReplicas {
		return fmt.Errorf("updatedAvailableReplicas got %d, expected %d", cls.Status.UpdatedAvailableReplicas, updatedAvailableReplicas)
	}

	return nil
}

func updateCollaSetWithRetry(c client.Client, namespace, name string, updateFn func(cls *appsv1alpha1.CollaSet) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cls := &appsv1alpha1.CollaSet{}
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, cls); err != nil {
			return err
		}

		if !updateFn(cls) {
			return nil
		}

		return c.Update(context.TODO(), cls)
	})
}

func updatePodWithRetry(c client.Client, namespace, name string, updateFn func(pod *corev1.Pod) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod := &corev1.Pod{}
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			return err
		}

		if !updateFn(pod) {
			return nil
		}

		return c.Update(context.TODO(), pod)
	})
}

func updatePodStatusWithRetry(c client.Client, namespace, name string, updateFn func(pod *corev1.Pod) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod := &corev1.Pod{}
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			return err
		}

		if !updateFn(pod) {
			return nil
		}

		return c.Status().Update(context.TODO(), pod)
	})
}

func testReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request, 5)
	fn := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(ctx, req)
		if _, done := ctx.Deadline(); !done && len(requests) == 0 {
			requests <- req
		}
		return result, err
	})
	return fn, requests
}

func TestCollaSetController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CollaSetController Test Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ControlPlane: envtest.ControlPlane{
			APIServer: &envtest.APIServer{
				URL: &url.URL{
					Host: "127.0.0.1:64431",
				},
			},
		},
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
		Scheme:             sch,
	})
	Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	var r reconcile.Reconciler
	r, request = testReconcile(NewReconciler(mgr))
	err = AddToMgr(mgr, r)
	Expect(err).NotTo(HaveOccurred())
	r, request = testReconcile(poddeletion.NewReconciler(mgr))
	err = poddeletion.AddToMgr(mgr, r)
	Expect(err).NotTo(HaveOccurred())

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
