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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/collaset/synccontrol"
	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
	"kusionstack.io/kuperator/pkg/controllers/poddecoration"
	"kusionstack.io/kuperator/pkg/controllers/poddeletion"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/strategy"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/features"
	"kusionstack.io/kuperator/pkg/utils/feature"
	"kusionstack.io/kuperator/pkg/utils/inject"
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

	It("replace pod with update", func() {
		for _, updateStrategy := range []appsv1alpha1.PodUpdateStrategyType{appsv1alpha1.CollaSetRecreatePodUpdateStrategyType, appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType, appsv1alpha1.CollaSetReplacePodUpdateStrategyType} {
			testcase := fmt.Sprintf("test-replace-pod-with-%s-update", strings.ToLower(string(updateStrategy)))
			Expect(createNamespace(c, testcase)).Should(BeNil())
			csName := fmt.Sprintf("foo-%s", strings.ToLower(string(updateStrategy)))
			cs := &appsv1alpha1.CollaSet{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testcase,
					Name:      csName,
				},
				Spec: appsv1alpha1.CollaSetSpec{
					Replicas: int32Pointer(1),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": csName,
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": csName,
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  csName,
									Image: "nginx:v1",
								},
							},
						},
					},
					UpdateStrategy: appsv1alpha1.UpdateStrategy{
						PodUpdatePolicy:       updateStrategy,
						OperationDelaySeconds: int32Pointer(1),
					},
				},
			}

			Expect(c.Create(context.TODO(), cs)).Should(BeNil())
			var originPodName, replaceNewPodName, replaceNewUpdatedPodName string

			podList := &corev1.PodList{}
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 1
			}, 5*time.Second, 1*time.Second).Should(BeTrue())
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Eventually(func() error { return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0) }, 5*time.Second, 1*time.Second).Should(BeNil())

			// label pod to trigger replace
			originPod := podList.Items[0]
			originPodName = originPod.Name
			Expect(updatePodWithRetry(c, originPod.Namespace, originPodName, func(pod *corev1.Pod) bool {
				pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
				return true
			})).Should(BeNil())
			Eventually(func() error {
				return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)
			}, 30*time.Second, 1*time.Second).Should(BeNil())

			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items) == 2
			}, 5*time.Second, 1*time.Second).Should(BeTrue())

			for _, pod := range podList.Items {
				if pod.Name == originPodName {
					continue
				}

				Expect(pod.Labels[appsv1alpha1.PodCreatingLabel]).ShouldNot(BeEquivalentTo(""))
			}

			// update collaset with recreate update
			observedGeneration := cs.Status.ObservedGeneration
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.UpdateStrategy.PodUpdatePolicy = updateStrategy
				cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
				return true
			})).Should(BeNil())
			Expect(observedGeneration != cs.Status.ObservedGeneration)
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			Eventually(func() error {
				if updateStrategy == appsv1alpha1.CollaSetReplacePodUpdateStrategyType {
					return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 0, 0, 0, 0)
				} else {
					return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 0, 1, 0, 0)
				}
			}, 5*time.Second, 1*time.Second).Should(BeNil())

			// allow origin pod to update
			Expect(updatePodWithRetry(c, originPod.Namespace, originPodName, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
			if updateStrategy == appsv1alpha1.CollaSetReplacePodUpdateStrategyType {
				Eventually(func() error {
					return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 0, 0, 0, 0)
				}, 10*time.Second, 1*time.Second).Should(BeNil())
			} else {
				Eventually(func() error {
					return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 0, 1, 0, 0)
				}, 10*time.Second, 1*time.Second).Should(BeNil())
			}
			// allow replaceNewPod to delete
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					if _, exist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
						replaceNewPodName = pod.Name
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					}
					return true
				})).Should(BeNil())
			}

			// wait for replaceNewPod deleted and replaceNewUpdatedPod created
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				for _, pod := range podList.Items {
					if pod.Name == replaceNewPodName {
						return false
					}
				}
				return true
			}, 5*time.Second, time.Second).Should(BeTrue())
			Eventually(func() error {
				if updateStrategy == appsv1alpha1.CollaSetReplacePodUpdateStrategyType {
					return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 0, 0, 0)
				} else {
					return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 1, 0, 0)
				}
			}, 5*time.Second, 1*time.Second).Should(BeNil())

			// mock replaceNewUpdatedPod pod service available
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for _, pod := range podList.Items {
				if pod.Labels[appsv1alpha1.PodReplacePairOriginName] == originPodName {
					replaceNewUpdatedPodName = pod.Name
					Expect(pod.Spec.Containers[0].Image).Should(BeEquivalentTo("nginx:v2"))
					Expect(updatePodWithRetry(c, pod.Namespace, replaceNewUpdatedPodName, func(pod *corev1.Pod) bool {
						pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
						return true
					})).Should(BeNil())
				}
			}

			// wait for originPod is deleted
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				for _, pod := range podList.Items {
					if pod.Name == originPodName {
						// allow originPod pod to be deleted
						Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
							labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
							pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
							return true
						})).Should(BeNil())
						return false
					}
				}
				return true
			}, 5*time.Second, time.Second).Should(BeTrue())
			Eventually(func() error {
				return expectedStatusReplicas(c, cs, 0, 0, 1, 1, 1, 0, 0, 1)
			}, 5*time.Second, 1*time.Second).Should(BeNil())
		}
	})

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
		for _, pod := range podList.Items {
			Expect(pod.Labels[appsv1alpha1.PodCreatingLabel]).ShouldNot(BeEquivalentTo(""))
		}

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

	It("scaleIn cancel", func() {
		testcase := "test-scale-in-cancel"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: ptr.To(int32(2)),
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

		csList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), csList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(csList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())

		// scale in 1 pods
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = ptr.To(int32(1))
			return true
		})).Should(BeNil())

		// ensure pod is during scale in
		var podToScaleIn *corev1.Pod
		Eventually(func() bool {
			Expect(c.List(context.TODO(), csList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range csList.Items {
				pod := csList.Items[i]
				if podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, &pod) {
					podToScaleIn = &pod
					return true
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// scale out 1 replicas
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = ptr.To(int32(2))
			return true
		})).Should(BeNil())

		// allow scaleIn pod to delete
		Eventually(func() bool {
			triggerAllowed := false
			Expect(c.List(context.TODO(), csList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range csList.Items {
				pod := csList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					if podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, pod) {
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
						triggerAllowed = podToScaleIn.Name == pod.Name
					}
					return true
				})).Should(BeNil())
			}
			return triggerAllowed
		}, 30*time.Second, 1*time.Second).Should(BeTrue())

		// wait for scale in pod to be deleted
		Eventually(func() bool {
			Expect(c.List(context.TODO(), csList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for _, pod := range csList.Items {
				if pod.Name == podToScaleIn.Name {
					return false
				}
			}
			return true
		}, 30*time.Second, 1*time.Second).Should(BeEquivalentTo(true))

		// wait for scale finished
		Eventually(func() int {
			Expect(c.List(context.TODO(), csList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(csList.Items)
		}, 30*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
	})

	It("scaleIn Order", func() {
		testcase := "test-scale-in-order"
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

		// scale out 1 replicas
		podToReserved := &podList.Items[0]
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(2)
			return true
		})).Should(BeNil())
		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))

		// scale in 1 pods
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			return true
		})).Should(BeNil())

		// allow scaleIn pod to delete
		Eventually(func() bool {
			triggerAllowed := false
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					if podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, pod) {
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
						triggerAllowed = true
					}
					return true
				})).Should(BeNil())
			}
			return triggerAllowed
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// wait for scale in finished
		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(1))

		// check reserved pod name
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Expect(podList.Items[0].Name).To(BeEquivalentTo(podToReserved.Name))
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
		for _, partition := range []int32{4, 3, 2, 1, 0} {
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
				return expectedStatusReplicas(c, cs, 0, 0, 0, 4, 4-partition, 4-partition, 0, 0)
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
			Expect(updatedReplicas).Should(BeEquivalentTo(4 - partition))
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
		for i := range podList.Items {
			Expect(cleanPodLifecycleLabels(c, podList.Items[i].Namespace, podList.Items[i].Name)).Should(BeNil())
		}
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

			Eventually(func() bool {
				Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &pod)).Should(BeNil())
				if !podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &pod) {
					return false
				}
				// allow Pod to do update
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
				return true
			}, 10*time.Second, 1*time.Second).Should(BeTrue())

			Eventually(func() error {
				// check updated pod replicas by CollaSet status
				return expectedStatusReplicas(c, cs, 0, 0, 0, 4, number, 1, 0, 0)
			}, 10*time.Second, 1*time.Second).Should(BeNil())

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

	It("scale succeed and update parallel", func() {
		testcase := "test-scale-and-update"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					RollingUpdate: &appsv1alpha1.RollingUpdateCollaSetStrategy{
						ByPartition: &appsv1alpha1.ByPartition{
							Partition: int32Pointer(1),
						},
					},
				},
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

		// update and scale out to 3 replicas
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(3)
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
			return true
		})).Should(BeNil())

		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(3))

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
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 2, 1, 0, 0)
		}, 10*time.Second, 1*time.Second).Should(BeNil())

		// delete updated pods to trigger scale out
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for _, pod := range podList.Items {
			if pod.Labels[appsv1.ControllerRevisionHashLabelKey] == cs.Status.UpdatedRevision {
				Expect(c.Delete(ctx, &pod)).Should(BeNil())
			}
		}

		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 2, 0, 0, 0)
		}, 10*time.Second, 1*time.Second).Should(BeNil())

	})

	It("scale failed and update parallel", func() {
		testcase := "test-scale-failed-and-update"
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
							},
						},
					},
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(BeNil())

		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(3))
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)).Should(BeNil())

		// update collaset with non-exist pvc by partition=1
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByPartition: &appsv1alpha1.ByPartition{
					Partition: int32Pointer(1),
				},
			}
			cls.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:  "foo",
					Image: "nginx:v1",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "not-exist",
							MountPath: "/tmp/not-exist",
						},
					},
				},
			}
			return true
		})).Should(BeNil())
		// allow Pod to do update
		Eventually(func() int32 {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			return cs.Status.OperatingReplicas
		}, 10*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
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
		// scaling out failed with non-exist pvc
		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 10*time.Second, 1*time.Second).Should(BeEquivalentTo(1))
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 1, 0, 0, 0, 0)).Should(BeNil())

		// update cls with good pod template
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		// allow Pod to do update
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
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
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByPartition: &appsv1alpha1.ByPartition{
					Partition: int32Pointer(1),
				},
			}
			cls.Spec.Template.Spec.Containers = []corev1.Container{
				{
					Name:  "foo",
					Image: "nginx:v1",
				},
			}
			return true
		})).Should(BeNil())
		Eventually(func() int {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items)
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(3))
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)).Should(BeNil())
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

		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for k := range podList.Items[0].Labels {
				if strings.HasPrefix(k, appsv1alpha1.PodOperatingLabelPrefix) {
					return true
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

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
		Expect(pod.Labels[appsv1alpha1.PodCompletingLabel]).ShouldNot(BeEquivalentTo(""))
		Expect(pod.Labels[appsv1alpha1.PodCreatingLabel]).Should(BeEquivalentTo(""))

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

		Eventually(func() int {
			count := 0
			for i := range podList.Items {
				pod := &podList.Items[i]
				Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)).Should(BeNil())
				for k := range pod.Labels {
					if strings.HasPrefix(k, appsv1alpha1.PodOperatingLabelPrefix) {
						count++
						// allow Pod to update
						Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
							labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
							pod.Labels[labelOperate] = "true"
							return true
						})).Should(BeNil())
					}
				}

			}
			return count
		}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(1))

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

	It("[replace update] reconcile by partition", func() {
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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
		for _, partition := range []int32{4, 3, 2, 1, 0} {
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
				return expectedStatusReplicas(c, cs, 0, 0, 0, 8-partition, 4-partition, 0, 0, 0)
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
			Expect(updatedReplicas).Should(BeEquivalentTo(4 - partition))
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

	It("[replace update] reconcile by label", func() {
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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
		var replacePairNewId, newPodInstanceId, newCreatePodName string
		for _, pod := range podList.Items {
			Expect(pod.Labels).ShouldNot(BeNil())
			// replace by current revision
			Expect(pod.Labels[appsv1.ControllerRevisionHashLabelKey]).Should(BeEquivalentTo(cs.Status.CurrentRevision))
			Expect(pod.Spec.Containers[0].Image).Should(BeEquivalentTo("nginx:v1"))
			if pod.Name == replacePod.Name {
				replacePairNewId = pod.Labels[appsv1alpha1.PodReplacePairNewId]
				Expect(replacePairNewId).ShouldNot(BeNil())
			} else {
				newCreatePodName = pod.Name
				newPodInstanceId = pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
				Expect(pod.Labels[appsv1alpha1.PodReplacePairOriginName]).Should(BeEquivalentTo(replacePod.Name))
			}
		}
		Expect(replacePairNewId).ShouldNot(BeEquivalentTo(""))
		Expect(newPodInstanceId).ShouldNot(BeEquivalentTo(""))
		Expect(newPodInstanceId).Should(BeEquivalentTo(replacePairNewId))
		Expect(newCreatePodName).ShouldNot(BeEquivalentTo(""))

		Expect(updatePodWithRetry(c, replacePod.Namespace, newCreatePodName, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 1, 2, 0, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for _, pod := range podList.Items {
			Expect(pod.Labels).ShouldNot(BeNil())
			// replace by current revision
			Expect(pod.Labels[appsv1.ControllerRevisionHashLabelKey]).Should(BeEquivalentTo(cs.Status.CurrentRevision))
			Expect(pod.Spec.Containers[0].Image).Should(BeEquivalentTo("nginx:v1"))
			if pod.Name == replacePod.Name {
				_, exist := pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]
				Expect(exist).Should(BeTrue())
			}
		}
	})

	It("replace pod with update", func() {
		updateStrategy := appsv1alpha1.CollaSetReplacePodUpdateStrategyType
		testcase := "test-replace-update-pod-from-v1-to-v3"
		Expect(createNamespace(c, testcase)).Should(Succeed())
		csName := fmt.Sprintf("foo-%s", strings.ToLower(string(updateStrategy)))
		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      csName,
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
				},
			},
		}

		Expect(c.Create(context.TODO(), cs)).Should(Succeed())
		var originpodName, replaceNewpodV2Name, replaceNewpodV3Name string

		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(Succeed())
			return len(podList.Items) == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(Succeed())
		Eventually(func() error { return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0) }, 5*time.Second, 1*time.Second).Should(Succeed())

		originpod := podList.Items[0]
		originpodName = originpod.Name

		// update collaset with replace update from v1 to v2
		observedGeneration := cs.Status.ObservedGeneration
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.PodUpdatePolicy = updateStrategy
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
			return true
		})).Should(Succeed())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(Succeed())
			return cs.Status.ObservedGeneration != observedGeneration
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// check replace new V2 pod
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(Succeed())
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(Succeed())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		for _, pod := range podList.Items {
			if pod.Name == originpodName {
				replaceUpdateRevision, _ := pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
				Expect(replaceUpdateRevision).To(BeEquivalentTo(cs.Status.UpdatedRevision))
				continue
			}
			replaceNewpodV2Name = pod.Name
			Expect(pod.Labels[appsv1alpha1.PodCreatingLabel]).ShouldNot(BeEquivalentTo(""))
		}
		Expect(len(replaceNewpodV2Name) > 0).To(BeTrue())

		// update collaset with replace update from v2 to v3
		observedGeneration = cs.Status.ObservedGeneration
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.PodUpdatePolicy = updateStrategy
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v3"
			return true
		})).Should(Succeed())
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(Succeed())
			return cs.Status.ObservedGeneration != observedGeneration
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// allow replace v2 pod to delete
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(Succeed())
		for i := range podList.Items {
			pod := &podList.Items[i]
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				if _, exist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				}
				return true
			})).Should(Succeed())
		}

		// check replace new V3 pod
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(Succeed())
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(Succeed())
		Expect(len(podList.Items)).Should(BeEquivalentTo(2))
		for _, pod := range podList.Items {
			if pod.Name == originpodName {
				replaceUpdateRevision, _ := pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
				Expect(replaceUpdateRevision).To(BeEquivalentTo(cs.Status.UpdatedRevision))
				continue
			} else if pod.Name == replaceNewpodV2Name {
				continue
			}
			replaceNewpodV3Name = pod.Name
			Expect(pod.Labels[appsv1alpha1.PodCreatingLabel]).ShouldNot(BeEquivalentTo(""))
		}
		Expect(len(replaceNewpodV3Name) > 0).To(BeTrue())
	})

	It("delete origin pod when replace", func() {
		testcase := "delete-origin-pod-when-replace"
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		replacePod := podList.Items[0]
		// label pod to trigger replace
		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodOperateLabelPrefix+"/pod-delete"] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for _, pod := range podList.Items {
				if pod.Name == replacePod.Name {
					return false
				}
			}

			return true
		}, 30*time.Second, 1*time.Second).Should(BeTrue())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		var newPod *corev1.Pod
		for i, pod := range podList.Items {
			if pod.Name != replacePod.Name {
				newPod = &podList.Items[i]
			}
		}
		Expect(newPod.Labels[appsv1alpha1.PodReplacePairOriginName]).Should(BeEquivalentTo(""))
	})

	It("delete new pod when replace", func() {
		testcase := "delete-new-pod-when-replace"
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		replacePod := podList.Items[0]
		// label pod to trigger replace
		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		var newPod *corev1.Pod
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if podList.Items[i].Name != replacePod.Name {
				newPod = &podList.Items[i]
			}
		}

		Expect(updatePodWithRetry(c, newPod.Namespace, newPod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		Expect(updatePodWithRetry(c, newPod.Namespace, newPod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodOperateLabelPrefix+"/pod-delete"] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i, pod := range podList.Items {
				if pod.Name == newPod.Name {
					return false
				}

				if pod.Name != replacePod.Name {
					newPod = &podList.Items[i]
				}
			}

			return true
		}, 30*time.Second, 1*time.Second).Should(BeTrue())

		for i, pod := range podList.Items {
			if pod.Name != replacePod.Name {
				newPod = &podList.Items[i]
			} else {
				replacePod = podList.Items[i]
			}
		}

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		Expect(newPod.Labels[appsv1alpha1.PodReplacePairOriginName]).Should(BeEquivalentTo(replacePod.Name))
		Expect(replacePod.Labels[appsv1alpha1.PodReplacePairNewId]).Should(BeEquivalentTo(newPod.Labels[appsv1alpha1.PodInstanceIDLabelKey]))
	})

	It("delete new pod by scale strategy when replace", func() {
		testcase := "delete-new-pod-by-strategy-when-replace"
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
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		replacePod := podList.Items[0]
		// label pod to trigger replace
		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		var newPod *corev1.Pod
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if podList.Items[i].Name != replacePod.Name {
				newPod = &podList.Items[i]
			}
		}

		// mock new pod service available
		Expect(updatePodWithRetry(c, newPod.Namespace, newPod.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
			return true
		})).Should(BeNil())

		// add finalizer for origin pod, to block deletion
		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			pod.Finalizers = append(pod.Finalizers, "block/deletion")
			return true
		})).Should(BeNil())

		// allow origin pod to delete
		Eventually(func() bool {
			Expect(c.Get(ctx, types.NamespacedName{Namespace: replacePod.Namespace, Name: replacePod.Name}, &replacePod)).Should(BeNil())
			if _, exist := replacePod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; exist {
				Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodOperateLabelPrefix+"/pod-delete"] = "true"
					return true
				})).Should(BeNil())
				return true
			}
			return false
		}, 10*time.Second, time.Second).Should(BeTrue())

		// ensure origin pod is terminating
		Eventually(func() bool {
			Expect(c.Get(ctx, types.NamespacedName{Namespace: replacePod.Namespace, Name: replacePod.Name}, &replacePod)).Should(BeNil())
			return replacePod.DeletionTimestamp != nil
		}, 10*time.Second, time.Second).Should(BeTrue())

		// selective scaleIn new pod
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(0)
			cls.Spec.ScaleStrategy.PodToDelete = []string{newPod.Name}
			return true
		})).Should(BeNil())

		// allow new pod to scaleIn
		Expect(updatePodWithRetry(c, newPod.Namespace, newPod.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		// new pod is not allowed to scaleIn if origin pod terminating
		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 1, 1, 1, 0, 0, 1)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		// remove finalizer from origin pod
		Expect(updatePodWithRetry(c, replacePod.Namespace, replacePod.Name, func(pod *corev1.Pod) bool {
			pod.Finalizers = []string{}
			return true
		})).Should(BeNil())

		// wait for pods are deleted
		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 0, 0, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		// check resource context
		rc := &appsv1alpha1.ResourceContext{}
		Expect(errors.IsNotFound(c.Get(ctx, types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, rc))).Should(BeTrue())
	})

	It("cancel pod replace", func() {
		testcase := "test-cancel-pod-replace"
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

		// try to replace pod
		podToReplace := &podList.Items[0]
		Expect(updatePodWithRetry(c, podToReplace.Namespace, podToReplace.Name, func(pod *corev1.Pod) bool {
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		// wait for new replace pod created
		podNewCreate := &corev1.Pod{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		for i := range podList.Items {
			if podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName] == podToReplace.Name {
				podNewCreate = &podList.Items[i]
			}
		}
		Expect(podNewCreate).ShouldNot(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())

		// cancel replace
		Expect(updatePodWithRetry(c, podToReplace.Namespace, podToReplace.Name, func(pod *corev1.Pod) bool {
			delete(pod.Labels, appsv1alpha1.PodReplaceIndicationLabelKey)
			return true
		})).Should(BeNil())

		// allow new replace pod to delete
		Expect(updatePodWithRetry(c, podNewCreate.Namespace, podNewCreate.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		// wait for replace canceled
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 1
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)).Should(BeNil())
		Expect(podList.Items[0].Name).Should(BeEquivalentTo(podToReplace.Name))
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
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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
		originPod1 := podList.Items[0]
		originPod2 := podList.Items[1]

		// label originPod1 to trigger update
		Expect(updatePodWithRetry(c, originPod1.Namespace, originPod1.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] = "true"
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 1, 0, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.UpdateStrategy.PodUpdatePolicy = appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType
			return true
		})).Should(BeNil())

		Eventually(func() bool {
			pod := &corev1.Pod{}
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: originPod1.Namespace, Name: originPod1.Name}, pod)).Should(BeNil())
			Expect(pod.Labels).ShouldNot(BeNil())
			_, replaceIndicate := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			_, replaceByUpdate := pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
			// check updated pod replicas by CollaSet status
			return replaceIndicate && replaceByUpdate
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// label originPod2 to trigger update
		Expect(updatePodWithRetry(c, originPod2.Namespace, originPod2.Name, func(pod *corev1.Pod) bool {
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[appsv1alpha1.CollaSetUpdateIndicateLabelKey] = "true"
			return true
		})).Should(BeNil())

		// allow originPod2 to do inPlace update
		Eventually(func() bool {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: originPod2.Namespace, Name: originPod2.Name}, &originPod2)).Should(BeNil())
			for k := range originPod2.Labels {
				if strings.HasPrefix(k, appsv1alpha1.PodOperatingLabelPrefix) {
					return true
				}
			}
			return false
		}, time.Second*10, time.Second).Should(BeTrue())
		Expect(updatePodWithRetry(c, originPod2.Namespace, originPod2.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		Eventually(func() error {
			// check updated pod replicas by CollaSet status
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 2, 2, 0, 0)
		}, 30*time.Second, 1*time.Second).Should(BeNil())

		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			pod := podList.Items[i]
			// originPod1 is during inPlaceUpdate lifecycle, but continue to be replaced by updated pod
			// originPod2 is during inPlaceUpdate lifecycle, and being updated by inPlace
			if pod.Name == originPod1.Name || pod.Name == originPod2.Name {
				Expect(podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, &pod)).Should(BeTrue())
			}
		}
	})

	It("[pvc template] provision", func() {
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
			Eventually(func() int {
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				activePvcs = make([]*corev1.PersistentVolumeClaim, 0)
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(8))
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
			// mock opsLifecycle webhook to allow Pod scale in
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
			Eventually(func() int {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items)
			}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())
			// there should be 4 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Eventually(func() int {
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				activePvcs = make([]*corev1.PersistentVolumeClaim, 0)
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(4))
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
			Eventually(func() int {
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				activePvcs = make([]*corev1.PersistentVolumeClaim, 0)
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(8))
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

	It("[pvc template] update", func() {
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
		for _, partition := range []int32{3, 2, 1, 0} {
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
			// mock opsLifecycle webhook to allow Pod update
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
			// there should be 6 pvcs
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Eventually(func() int {
				allPvcs := &corev1.PersistentVolumeClaimList{}
				activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(6))
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

	It("[pvc template] replace update", func() {
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
		for _, partition := range []int32{2, 1, 0} {
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
			Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
				cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
					ByPartition: &appsv1alpha1.ByPartition{
						Partition: &partition,
					},
				}
				cls.Spec.UpdateStrategy.PodUpdatePolicy = appsv1alpha1.CollaSetReplacePodUpdateStrategyType
				cls.Spec.ScaleStrategy.OperationDelaySeconds = int32Pointer(1)
				cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
				return true
			})).Should(BeNil())
			// wait for reconcile finished
			Eventually(func() int32 {
				Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
				return cs.Status.UpdatedReplicas
			}, time.Second*10, time.Second).Should(BeEquivalentTo(3 - partition))
			// there should be 1 new pod, mock new pod service available
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for _, pod := range podList.Items {
				if _, exist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
					Expect(pod.Spec.Containers[0].Image).Should(BeEquivalentTo("nginx:v2"))
					Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
						pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
						//replaceNewPod++
						return true
					})).Should(BeNil())
				}
			}
			// allow Pod to delete
			podList := &corev1.PodList{}
			Eventually(func() bool {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				for i := range podList.Items {
					pod := &podList.Items[i]
					if _, exist := pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]; exist {
						Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
							labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
							pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
							return true
						})).Should(BeNil())
						return true
					}
				}
				return false
			}, time.Second*10, time.Second).Should(BeTrue())
			// wait for replace finished
			Eventually(func() error {
				return expectedStatusReplicas(c, cs, 0, 0, 3-partition, 3, 3-partition, 0, 0, 3-partition)
			}, 10*time.Second, time.Second).Should(BeNil())
			// there should be 6 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			Eventually(func() int {
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				activePvcs = make([]*corev1.PersistentVolumeClaim, 0)
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(6))
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

	It("[pvc template] retention policy", func() {
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
		csRecreate := cs.DeepCopy()
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
			Eventually(func() int {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items)
			}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(2))
			// there should be 8 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			pvcNames := sets.String{}
			Eventually(func() int {
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				activePvcs = make([]*corev1.PersistentVolumeClaim, 0)
				pvcNames = sets.String{}
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
					pvcNames.Insert(allPvcs.Items[i].Name)

				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(8))
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
			// delete collaset
			Expect(c.Delete(context.TODO(), cs)).Should(BeNil())
			Eventually(func() bool {
				err := c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)
				return errors.IsNotFound(err)
			}, time.Second*10, time.Second).Should(BeTrue())
			// delete collaset pods
			podList := &corev1.PodList{}
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				Expect(c.Delete(context.TODO(), &podList.Items[i])).Should(BeNil())
			}
			Eventually(func() int {
				Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
				return len(podList.Items)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(0))
			// there should be 8 pvcs
			allPvcs := &corev1.PersistentVolumeClaimList{}
			activePvcs := make([]*corev1.PersistentVolumeClaim, 0)
			pvcNames := sets.String{}
			Eventually(func() int {
				Expect(c.List(context.TODO(), allPvcs, client.InNamespace(cs.Namespace))).Should(BeNil())
				activePvcs = make([]*corev1.PersistentVolumeClaim, 0)
				pvcNames = sets.String{}
				for i := range allPvcs.Items {
					if allPvcs.Items[i].DeletionTimestamp != nil {
						continue
					}
					activePvcs = append(activePvcs, &allPvcs.Items[i])
					pvcNames.Insert(allPvcs.Items[i].Name)
				}
				return len(activePvcs)
			}, time.Second*10, time.Second).Should(BeEquivalentTo(8))
			// recreate collaset with same name
			csRecreate.Spec.ScaleStrategy.PersistentVolumeClaimRetentionPolicy = &appsv1alpha1.PersistentVolumeClaimRetentionPolicy{
				WhenDeleted: appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType,
			}
			Expect(c.Create(context.TODO(), csRecreate)).Should(BeNil())
			Eventually(func() int {
				Expect(c.List(context.TODO(), podList, client.InNamespace(csRecreate.Namespace))).Should(BeNil())
				return len(podList.Items)
			}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(4))
			Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: csRecreate.Namespace, Name: csRecreate.Name}, csRecreate)).Should(BeNil())
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

	It("podToDelete", func() {
		testcase := "test-pod-to-delete"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		_ = feature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=%s", features.ReclaimPodScaleStrategy, "true"))
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

		// delete 1 pod
		toDeleteName := podList.Items[0].Name
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToDelete: []string{toDeleteName},
			}
			return true
		})).Should(BeNil())

		// mark pod allowed to operate in PodDeletePodOpsLifecycle
		pod := &podList.Items[0]
		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		// there should be 2 pods eventually and toDeletePod do not exist
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			// toDeleteName do not exist
			for i := range podList.Items {
				if podList.Items[i].Name == toDeleteName {
					return false
				}
			}
			return len(podList.Items) == 2
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// cls.spec.scaleStrategy.podToDelete should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToDelete)).Should(BeEquivalentTo(0))
	})

	It("[podToDelete] scale", func() {
		testcase := "test-pod-to-delete-scale"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		_ = feature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=%s", features.ReclaimPodScaleStrategy, "true"))
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

		// delete 1 pod, and scale in to replicas - 1
		toDeleteName := podList.Items[0].Name
		reservedName := podList.Items[1].Name
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToDelete: []string{toDeleteName},
			}
			return true
		})).Should(BeNil())

		// mark pod allowed to operate in ScaleInPodOpsLifecycle
		pod := &podList.Items[0]
		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		// there should be 1 pod eventually, and origin reserved pod exists
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			reserved := false
			for i := range podList.Items {
				// toDeleteName do not exist
				if podList.Items[i].Name == toDeleteName {
					return false
				}
				// reservedName exist
				if podList.Items[i].Name == reservedName {
					reserved = true
				}
			}
			return len(podList.Items) == 1 && reserved
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// cls.spec.scaleStrategy.podToDelete should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToDelete)).Should(BeEquivalentTo(0))

		// delete 1 pod, and scale out to replicas + 1
		toDeleteName = podList.Items[0].Name
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(3)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToDelete: []string{toDeleteName},
			}
			return true
		})).Should(BeNil())

		// mark pod allowed to operate in PodDeletePodOpsLifecycle
		pod = &podList.Items[0]
		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
			pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		// there should be 3 pods eventually and toDeletePod do not exist
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				// toDeleteName do not exist
				if podList.Items[i].Name == toDeleteName {
					return false
				}
			}
			return len(podList.Items) == 3
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// cls.spec.scaleStrategy.podToDelete should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToDelete)).Should(BeEquivalentTo(0))
	})

	It("Exclude and Include pod", func() {
		testcase := "test-pod-to-exclude-include"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		_ = feature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=%s", features.ReclaimPodScaleStrategy, "true"))
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
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "pvc",
										MountPath: "/tmp/pvc",
									},
								},
							},
						},
					},
				},
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc",
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
		pvcList := &corev1.PersistentVolumeClaimList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())

		// exclude 1 pod, and scale in to replicas - 1
		toExcludeName := podList.Items[0].Name
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToExclude: []string{toExcludeName},
			}
			return true
		})).Should(BeNil())

		// excluded pod should not have ownerReference
		var excludedPodID string
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			podExcluded := false
			for i := range podList.Items {
				pod := podList.Items[i]
				if pod.Name == toExcludeName {
					podExcluded = len(pod.OwnerReferences) == 0
					podExcluded = podExcluded && pod.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == "true"
					excludedPodID = pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
					break
				}
			}
			return podExcluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// excluded pvc should not have ownerReference
		Eventually(func() bool {
			pvcExcluded := false
			Expect(c.List(context.TODO(), pvcList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range pvcList.Items {
				pvc := pvcList.Items[i]
				if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
					pvcExcluded = len(pvc.OwnerReferences) == 0
					pvcExcluded = pvcExcluded && pvc.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == "true"
					break
				}
			}
			return pvcExcluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// cls.spec.scaleStrategy.podToExclude should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToExclude)).Should(BeEquivalentTo(0))

		// wait for collaset reconcile finished
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// include 1 pod, and scale out to replicas + 1
		toIncludeName := toExcludeName
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(2)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToExclude: []string{},
				PodToInclude: []string{toIncludeName},
			}
			return true
		})).Should(BeNil())

		// included pod should not have ownerReference
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			podIncluded := false
			//var podId string
			for i := range podList.Items {
				pod := podList.Items[i]
				if pod.Name == toExcludeName {
					podIncluded = len(pod.OwnerReferences) == 1
					podIncluded = podIncluded && pod.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == ""
					excludedPodID = pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
					break
				}
			}
			return podIncluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// included pvc should not have ownerReference
		Eventually(func() bool {
			pvcIncluded := false
			Expect(c.List(context.TODO(), pvcList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range pvcList.Items {
				pvc := pvcList.Items[i]
				if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
					pvcIncluded = len(pvc.OwnerReferences) == 1
					pvcIncluded = pvcIncluded && pvc.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == ""
					break
				}
			}
			return pvcIncluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// wait for cls.spec.scaleStrategy.podToInclude should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToInclude)).Should(BeEquivalentTo(0))

		// wait for collaset reconcile finished
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())
	})

	It("[Exclude] replace origin pod", func() {
		testcase := "test-exclude-replace-pod"
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
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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

		// label pod to trigger replace
		originPod := podList.Items[0]
		Expect(updatePodWithRetry(c, originPod.Namespace, originPod.Name, func(pod *corev1.Pod) bool {
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// exclude origin pod, and scale in to replicas - 1
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToExclude: []string{originPod.Name},
			}
			return true
		})).Should(BeNil())

		// no pod is excluded and scale in
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// mock newPod service available
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for _, pod := range podList.Items {
			if pod.Labels[appsv1alpha1.PodReplacePairOriginName] == originPod.Name {
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
			}
		}

		// wait for originPod is deleted
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for _, pod := range podList.Items {
				if pod.Name == originPod.Name {
					// allow originPod pod to be deleted
					Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
						return true
					})).Should(BeNil())
					return true
				}
			}
			return false
		}, 5*time.Second, time.Second).Should(BeTrue())
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 1, 2, 2, 1, 0, 1)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// allow Pods to scaleIn
		Eventually(func() bool {
			triggerAllowed := false
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := podList.Items[i]
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					if podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, pod) {
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
						triggerAllowed = true
					}
					return true
				})).Should(BeNil())
			}
			return triggerAllowed
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// wait for replace completed
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// origin pod is deleted
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Expect(len(podList.Items)).Should(BeEquivalentTo(1))
		Expect(podList.Items[0].Name).ShouldNot(BeEquivalentTo(originPod.Name))
	})

	It("[Exclude] replace new pod", func() {
		testcase := "test-exclude-new-pod"
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
				UpdateStrategy: appsv1alpha1.UpdateStrategy{
					OperationDelaySeconds: int32Pointer(1),
					PodUpdatePolicy:       appsv1alpha1.CollaSetReplacePodUpdateStrategyType,
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

		// label pod to trigger replace
		originPod := podList.Items[0]
		Expect(updatePodWithRetry(c, originPod.Namespace, originPod.Name, func(pod *corev1.Pod) bool {
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// find newPod
		newPod := &corev1.Pod{}
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Eventually(func() bool {
			for i := range podList.Items {
				pod := podList.Items[i]
				if pod.Labels[appsv1alpha1.PodReplacePairOriginName] != "" {
					newPod = &pod
					return true
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// exclude new pod, and scale in to replicas - 1
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToExclude: []string{newPod.Name},
			}
			return true
		})).Should(BeNil())

		// no pod is excluded and scale in
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 3, 3, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// mock newPod service available
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for _, pod := range podList.Items {
			if pod.Labels[appsv1alpha1.PodReplacePairOriginName] == originPod.Name {
				Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
			}
		}

		// wait for originPod is deleted
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for _, pod := range podList.Items {
				if pod.Name == originPod.Name {
					// allow originPod pod to be deleted
					Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
						return true
					})).Should(BeNil())
					return true
				}
			}
			return false
		}, 5*time.Second, time.Second).Should(BeTrue())

		// wait for exclude completed
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// exclude pod is deleted
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		Expect(len(podList.Items)).Should(BeEquivalentTo(2))
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := podList.Items[i]
				if pod.Name == newPod.Name {
					return len(pod.OwnerReferences) == 0 && pod.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == "true"
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
	})

	It("[Include] pod without revision", func() {
		testcase := "test-include-pod-without-revision"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		_ = feature.DefaultMutableFeatureGate.Set(fmt.Sprintf("%s=%s", features.ReclaimPodScaleStrategy, "true"))
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
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "pvc",
										MountPath: "/tmp/pvc",
									},
								},
							},
						},
					},
				},
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "pvc",
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
		pvcList := &corev1.PersistentVolumeClaimList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())

		// exclude 1 pod, and scale in to replicas - 1
		toExcludeName := podList.Items[0].Name
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(1)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToExclude: []string{toExcludeName},
			}
			return true
		})).Should(BeNil())

		// excluded pod should not have ownerReference
		var excludedPodID string
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			podExcluded := false
			for i := range podList.Items {
				pod := podList.Items[i]
				if pod.Name == toExcludeName {
					podExcluded = len(pod.OwnerReferences) == 0
					podExcluded = podExcluded && pod.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == "true"
					excludedPodID = pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
					break
				}
			}
			return podExcluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// excluded pvc should not have ownerReference
		Eventually(func() bool {
			pvcExcluded := false
			Expect(c.List(context.TODO(), pvcList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range pvcList.Items {
				pvc := pvcList.Items[i]
				if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
					pvcExcluded = len(pvc.OwnerReferences) == 0
					pvcExcluded = pvcExcluded && pvc.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == "true"
					break
				}
			}
			return pvcExcluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// cls.spec.scaleStrategy.podToExclude should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToExclude)).Should(BeEquivalentTo(0))

		// wait for collaset reconcile finished
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 1, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// delete include pod's revision
		toIncludeName := toExcludeName
		Expect(updatePodWithRetry(c, cs.Namespace, toIncludeName, func(pod *corev1.Pod) bool {
			delete(pod.Labels, appsv1.ControllerRevisionHashLabelKey)
			return true
		})).Should(BeNil())

		// include 1 pod, and scale out to replicas + 1
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(2)
			cls.Spec.ScaleStrategy = appsv1alpha1.ScaleStrategy{
				PodToExclude: []string{},
				PodToInclude: []string{toIncludeName},
			}
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByPartition: &appsv1alpha1.ByPartition{
					Partition: int32Pointer(2),
				},
			}
			return true
		})).Should(BeNil())

		// included pod should have ownerReference
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			podIncluded := false
			//var podId string
			for i := range podList.Items {
				pod := podList.Items[i]
				if pod.Name == toExcludeName {
					podIncluded = len(pod.OwnerReferences) == 1
					podIncluded = podIncluded && pod.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == ""
					excludedPodID = pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
					break
				}
			}
			return podIncluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// included pvc should have ownerReference
		Eventually(func() bool {
			pvcIncluded := false
			Expect(c.List(context.TODO(), pvcList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range pvcList.Items {
				pvc := pvcList.Items[i]
				if pvc.Labels[appsv1alpha1.PodInstanceIDLabelKey] == excludedPodID {
					pvcIncluded = len(pvc.OwnerReferences) == 1
					pvcIncluded = pvcIncluded && pvc.Labels[appsv1alpha1.PodOrphanedIndicateLabelKey] == ""
					break
				}
			}
			return pvcIncluded
		}, 10*time.Second, 1*time.Second).Should(BeTrue())

		// wait for cls.spec.scaleStrategy.podToInclude should be reclaimed
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
		Expect(len(cs.Spec.ScaleStrategy.PodToInclude)).Should(BeEquivalentTo(0))

		// wait for collaset reconcile finished
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 1, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		// update collaset image to nginx:v2
		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
			cls.Spec.UpdateStrategy.RollingUpdate = &appsv1alpha1.RollingUpdateCollaSetStrategy{
				ByPartition: &appsv1alpha1.ByPartition{
					Partition: int32Pointer(0),
				},
			}
			return true
		})).Should(BeNil())

		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for k := range podList.Items[0].Labels {
				if strings.HasPrefix(k, appsv1alpha1.PodOperatingLabelPrefix) {
					return true
				}
			}
			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		// allow pod to update
		Expect(c.List(context.TODO(), pvcList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			pod := podList.Items[i]
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}

		// check include pod is recreate
		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 1, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			if len(podList.Items) != 2 {
				return false
			}
			for i := range podList.Items {
				if podList.Items[i].Name == toIncludeName {
					return false
				}
			}
			return true
		}, 10*time.Second, 1*time.Second).Should(BeTrue())
	})

	It("clean up CollaSet", func() {
		testcase := "test-collaset-cleanup"
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

		resourceContextList := &appsv1alpha1.ResourceContextList{}
		Expect(c.List(context.TODO(), resourceContextList, client.InNamespace(testcase))).Should(BeNil())
		Expect(len(resourceContextList.Items)).Should(BeEquivalentTo(1))
		Expect(len(resourceContextList.Items[0].Spec.Contexts)).Should(BeEquivalentTo(2))

		// delete this CollaSet
		Expect(c.Delete(context.TODO(), cs)).Should(BeNil())
		Eventually(func() bool {
			if err := c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs); err != nil && errors.IsNotFound(err) {
				return true
			}

			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		rc := &resourceContextList.Items[0]
		Eventually(func() bool {
			err := c.Get(context.TODO(), types.NamespacedName{Namespace: rc.Namespace, Name: rc.Name}, rc)
			return errors.IsNotFound(err)
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
	})

	It("clean up CollaSet with PodDecoration", func() {
		testcase := "test-collaset-cleanup-with-poddecoration"
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

		podDecoration := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cs.Namespace,
				Name:      "pd-foo",
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
						Partition: int32Pointer(0),
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar",
								Image: "nginx:v1",
								Command: []string{
									"sleep",
									"2h",
								},
							},
						},
					},
				},
			},
		}

		Expect(c.Create(context.TODO(), podDecoration)).Should(BeNil())

		// allow Pod to do update
		for i := range podList.Items {
			pod := podList.Items[i]
			Eventually(func() bool {
				Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &pod)).Should(BeNil())
				for k := range pod.Labels {
					if strings.HasPrefix(k, appsv1alpha1.PodOperatingLabelPrefix) {
						return true
					}
				}
				return false
			}, time.Second*10, time.Second).Should(BeTrue())
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.UpdateOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				return true
			})).Should(BeNil())
		}

		Eventually(func() int32 {
			err := c.Get(context.TODO(), types.NamespacedName{Namespace: podDecoration.Namespace, Name: podDecoration.Name}, podDecoration)
			if !errors.IsNotFound(err) {
				Expect(err).Should(BeNil())
			}
			return podDecoration.Status.UpdatedPods
		}, 30*time.Second, 3*time.Second).Should(BeEquivalentTo(2))

		Expect(expectedStatusReplicas(c, cs, 0, 0, 0, 2, 2, 0, 0, 0)).Should(BeNil())
		Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			Expect(len(podList.Items[i].OwnerReferences)).Should(BeEquivalentTo(2))
		}

		resourceContextList := &appsv1alpha1.ResourceContextList{}
		Expect(c.List(context.TODO(), resourceContextList, client.InNamespace(testcase))).Should(BeNil())
		Expect(len(resourceContextList.Items)).Should(BeEquivalentTo(1))
		Expect(len(resourceContextList.Items[0].Spec.Contexts)).Should(BeEquivalentTo(2))

		// delete this CollaSet
		Expect(c.Delete(context.TODO(), cs)).Should(BeNil())
		Eventually(func() bool {
			if err := c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs); err != nil && errors.IsNotFound(err) {
				return true
			}

			return false
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				if len(podList.Items[i].OwnerReferences) != 1 && podList.Items[i].OwnerReferences[0].Kind == "CollaSet" {
					return false
				}
			}
			return true
		}, 50*time.Second, 1*time.Second).Should(BeTrue())

		rc := &resourceContextList.Items[0]
		Eventually(func() bool {
			err := c.Get(context.TODO(), types.NamespacedName{Namespace: rc.Namespace, Name: rc.Name}, rc)
			return errors.IsNotFound(err)
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
	})

	It("pod create failed with PodDecoration", func() {
		testcase := "test-pod-create-failed-with-poddecoration"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		// create a bad podDecoration
		podDecoration := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "pd-foo",
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
						Partition: int32Pointer(0),
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Containers: []*appsv1alpha1.ContainerPatch{
						{
							InjectPolicy: appsv1alpha1.AfterPrimaryContainer,
							Container: corev1.Container{
								Name:  "sidecar",
								Image: "nginx:v1",
								Command: []string{
									"sleep",
									"2h",
								},
								VolumeMounts: []corev1.VolumeMount{
									{
										Name:      "non-exist",
										MountPath: "/non/exist",
									},
								},
							},
						},
					},
				},
			},
		}
		Expect(c.Create(context.TODO(), podDecoration)).Should(BeNil())

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

		// create pod failed
		Eventually(func() bool {
			err := c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).Should(BeNil())

			for _, cond := range cs.Status.Conditions {
				if cond.Reason == "ScaleOutFailed" {
					return true
				}
			}
			return false
		}, 30*time.Second, 3*time.Second).Should(BeTrue())

		// update podDecoration with good manner
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: podDecoration.Namespace, Name: podDecoration.Name}, podDecoration)).Should(BeNil())
		podDecoration.Spec.Template.Containers[0].VolumeMounts = []corev1.VolumeMount{}
		Expect(c.Update(ctx, podDecoration)).Should(BeNil())

		// check pod create succeeded
		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, 5*time.Second, 1*time.Second).Should(BeTrue())
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
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

func cleanPodLifecycleLabels(c client.Client, namespace, name string) error {
	pod := &corev1.Pod{}
	if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
		return err
	}
	newLabels := make(map[string]string)
	for k, v := range pod.Labels {
		has := false
		for _, prefix := range appsv1alpha1.WellKnownLabelPrefixesWithID {
			if strings.HasPrefix(k, prefix) {
				has = true
				break
			}
		}
		if !has {
			newLabels[k] = v
		}
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			return err
		}
		pod.Labels = newLabels
		return c.Update(context.TODO(), pod)
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
	// ignore PodDecoration SharedStrategyController
	strategy.SharedStrategyController.Synced()
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
	r, request = testReconcile(poddecoration.NewReconciler(mgr))
	err = poddecoration.AddToMgr(mgr, r)
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
