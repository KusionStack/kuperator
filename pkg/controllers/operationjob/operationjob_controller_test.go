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

package operationjob

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/collaset"
	"kusionstack.io/kuperator/pkg/controllers/poddeletion"
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

var _ = Describe("operationjob controller", func() {

	It("[replace] reconcile", func() {
		testcase := "test-replace"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		cs := createCollaSetWithReplicas("foo", testcase, 2)
		podNames := getPodNamesFromCollaSet(cs)

		oj := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action: appsv1alpha1.OpsActionReplace,
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: podNames[0],
					},
					{
						Name: podNames[1],
					},
				},
			},
		}

		Expect(c.Create(ctx, oj)).Should(BeNil())

		// wait for new pod created
		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, time.Second*10, time.Second).Should(BeTrue())

		// mock new pods serviceAvailable
		Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if _, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
				Expect(updatePodWithRetry(podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
			}
		}

		// allow origin pod to be deleted
		for i := range podList.Items {
			pod := &podList.Items[i]
			if _, exist := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; exist {
				Expect(updatePodWithRetry(pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
		}

		// wait for replace completed
		assertJobProgressSucceeded(oj, time.Second*5)
	})

	It("[replace] delete origin pod manually", func() {
		testcase := "test-delete-origin-pod"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		cs := createCollaSetWithReplicas("foo", testcase, 2)
		podNames := getPodNamesFromCollaSet(cs)

		oj := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action: appsv1alpha1.OpsActionReplace,
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: podNames[0],
					},
					{
						Name: podNames[1],
					},
				},
			},
		}

		Expect(c.Create(ctx, oj)).Should(BeNil())

		// wait for new pod created
		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, time.Second*10, time.Second).Should(BeTrue())

		// delete origin pod
		for i := range podList.Items {
			pod := podList.Items[i]
			if pod.Name == podNames[0] || pod.Name == podNames[1] {
				Expect(c.Delete(context.Background(), &pod)).Should(BeNil())
			}
		}
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, time.Second*10, time.Second).Should(BeTrue())

		// check opj is still in processing
		assertJobProgressProcessing(oj, time.Second*5)

		// mock new pods serviceAvailable
		Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			Expect(updatePodWithRetry(podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
				pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
				return true
			})).Should(BeNil())
		}

		// wait for replace completed
		assertJobProgressSucceeded(oj, time.Second*5)
	})

	It("[replace] by partition", func() {
		testcase := "test-replace-by-partition"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		cs := createCollaSetWithReplicas("foo", testcase, 3)
		podNames := getPodNamesFromCollaSet(cs)

		oj := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action:    appsv1alpha1.OpsActionReplace,
				Partition: int32Pointer(0),
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: podNames[0],
					},
					{
						Name: podNames[1],
					},
					{
						Name: podNames[2],
					},
				},
			},
		}

		Expect(c.Create(ctx, oj)).Should(BeNil())

		for _, partition := range []int32{0, 1, 2, 3} {
			// update partition
			Eventually(func() error {
				return c.Get(context.TODO(), types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
			}, time.Second*5, time.Second).Should(BeNil())
			Expect(updateOperationJobWithRetry(oj.Namespace, oj.Name, func(job *appsv1alpha1.OperationJob) bool {
				job.Spec.Partition = &partition
				return true
			})).Should(BeNil())

			// wait for new pod created
			podList := &corev1.PodList{}
			if partition > 0 {
				Eventually(func() int32 {
					Expect(c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)).Should(BeNil())
					processingPodCount := *oj.Spec.Partition - oj.Status.SucceededPodCount - oj.Status.FailedPodCount
					return processingPodCount
				}, time.Second*10, time.Second).Should(BeEquivalentTo(1))
				Eventually(func() bool {
					Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
					Expect(c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)).Should(BeNil())
					replacingPod := 0
					for i := range podList.Items {
						if name, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
							Expect(name).Should(BeEquivalentTo(oj.Spec.Targets[partition-1].Name))
							replacingPod++
						}
					}
					processingPodCount := *oj.Spec.Partition - oj.Status.SucceededPodCount - oj.Status.FailedPodCount
					return processingPodCount == int32(replacingPod)
				}, time.Second*10, time.Second).Should(BeTrue())
			}

			// mock new pods serviceAvailable
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				if _, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
					Expect(updatePodWithRetry(podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
						pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
						return true
					})).Should(BeNil())
				}
			}

			// allow origin pod to be deleted
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			for i := range podList.Items {
				pod := &podList.Items[i]
				if _, exist := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; exist {
					Expect(updatePodWithRetry(pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
						labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
						pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
						pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
						return true
					})).Should(BeNil())
				}
			}

			// assert operation progress
			assertSucceededReplicas(oj, partition, time.Second*5)
			if partition == 0 {
				c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
				assertJobProgressPending(oj, time.Second*5)
			} else if partition < 3 {
				assertJobProgressProcessing(oj, time.Second*5)
			} else {
				assertJobProgressSucceeded(oj, time.Second*5)
			}
		}

	})

	It("[replace] non-exist pod", func() {
		testcase := "test-replace-non-exist-pod"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		oj := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action: appsv1alpha1.OpsActionReplace,
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: "non-exist",
					},
				},
			},
		}

		Expect(c.Create(ctx, oj)).Should(BeNil())

		assertJobProgressFailed(oj, time.Second*5)
	})

	It("[replace] parallel", func() {
		testcase := "test-replace-parallel"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		cs := createCollaSetWithReplicas("foo", testcase, 2)
		podNames := getPodNamesFromCollaSet(cs)

		oj1 := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action: appsv1alpha1.OpsActionReplace,
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: podNames[0],
					},
					{
						Name: podNames[1],
					},
				},
			},
		}

		oj2 := oj1.DeepCopy()
		oj2.Name = "foo-copy"

		Expect(c.Create(ctx, oj1)).Should(BeNil())
		Expect(c.Create(ctx, oj2)).Should(BeNil())

		// wait for new pod created
		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, time.Second*10, time.Second).Should(BeTrue())

		// mock new pods serviceAvailable
		Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if _, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
				Expect(updatePodWithRetry(podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
			}
		}

		// allow origin pod to be deleted
		for i := range podList.Items {
			pod := &podList.Items[i]
			Expect(updatePodWithRetry(pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
				if _, exist := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; exist {
					pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
				}
				return true
			})).Should(BeNil())
		}

		// wait for oj1 and oj2 both completed
		assertJobProgressSucceeded(oj1, time.Second*5)
		assertJobProgressSucceeded(oj2, time.Second*5)
	})

	It("[replace] cancel", func() {
		testcase := "test-replace-cancel"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		cs := createCollaSetWithReplicas("foo", testcase, 1)
		podNames := getPodNamesFromCollaSet(cs)

		oj := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action: appsv1alpha1.OpsActionReplace,
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: podNames[0],
					},
				},
			},
		}

		Expect(c.Create(ctx, oj)).Should(BeNil())

		// wait for new pod created
		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 2
		}, time.Second*10, time.Second).Should(BeTrue())

		// delete operationJob and wait for deleted
		Expect(c.Delete(ctx, oj)).Should(BeNil())
		Eventually(func() bool {
			err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
			return errors.IsNotFound(err)
		}, time.Second*10, time.Second).Should(BeTrue())

		// allow new pod to be deleted
		for i := range podList.Items {
			pod := &podList.Items[i]
			if _, exist := pod.Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
				Expect(updatePodWithRetry(pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
		}

		// wait for replace canceled finished
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 1 && podList.Items[0].Name == podNames[0]
		}, time.Second*10, time.Second).Should(BeTrue())
	})

	It("deadline and ttl", func() {
		testcase := "test-deadline-ttl"
		Expect(createNamespace(c, testcase)).Should(BeNil())
		cs := createCollaSetWithReplicas("foo", testcase, 2)
		podNames := getPodNamesFromCollaSet(cs)

		oj := &appsv1alpha1.OperationJob{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "foo",
			},
			Spec: appsv1alpha1.OperationJobSpec{
				Action: appsv1alpha1.OpsActionReplace,
				Targets: []appsv1alpha1.PodOpsTarget{
					{
						Name: podNames[0],
					},
					{
						Name: podNames[1],
					},
				},
				ActiveDeadlineSeconds:   int32Pointer(10),
				TTLSecondsAfterFinished: int32Pointer(5),
			},
		}

		Expect(c.Create(ctx, oj)).Should(BeNil())

		// wait for new pod created
		podList := &corev1.PodList{}
		Eventually(func() bool {
			Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			return len(podList.Items) == 4
		}, time.Second*10, time.Second).Should(BeTrue())

		// mock only 1 new pod serviceAvailable
		Expect(c.List(ctx, podList, client.InNamespace(cs.Namespace))).Should(BeNil())
		for i := range podList.Items {
			if _, exist := podList.Items[i].Labels[appsv1alpha1.PodReplacePairOriginName]; exist {
				Expect(updatePodWithRetry(podList.Items[i].Namespace, podList.Items[i].Name, func(pod *corev1.Pod) bool {
					pod.Labels[appsv1alpha1.PodServiceAvailableLabel] = "true"
					return true
				})).Should(BeNil())
				break
			}
		}

		// allow origin pod to be deleted
		for i := range podList.Items {
			pod := &podList.Items[i]
			if _, exist := pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; exist {
				Expect(updatePodWithRetry(pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
					labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
					pod.Labels[labelOperate] = fmt.Sprintf("%d", time.Now().UnixNano())
					pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
					return true
				})).Should(BeNil())
			}
		}
		assertSucceededReplicas(oj, 1, time.Second*10)

		// wait for replace failed after ActiveDeadlineSeconds
		assertJobProgressFailed(oj, time.Second*10)
		assertFailedReplicas(oj, 1, time.Second*10)

		// wait for operationJob deleted after TTL
		Eventually(func() bool {
			err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
			if errors.IsNotFound(err) {
				return true
			} else {
				Expect(err).Should(BeNil())
			}
			return false
		}, time.Second*20, time.Second).Should(BeTrue())
	})

})

func assertFailedReplicas(oj *appsv1alpha1.OperationJob, failedPodCount int32, timeout time.Duration) {
	Eventually(func() bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
		if errors.IsNotFound(err) {
			return false
		} else {
			Expect(err).Should(BeNil())
		}
		return oj.Status.FailedPodCount == failedPodCount
	}, timeout, time.Second).Should(BeTrue())
}

func assertSucceededReplicas(oj *appsv1alpha1.OperationJob, succeededPodCount int32, timeout time.Duration) {
	Eventually(func() bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
		if errors.IsNotFound(err) {
			return false
		} else {
			Expect(err).Should(BeNil())
		}
		return oj.Status.SucceededPodCount == succeededPodCount
	}, timeout, time.Second).Should(BeTrue())
}

func assertJobProgressPending(oj *appsv1alpha1.OperationJob, timeout time.Duration) {
	Eventually(func() bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
		if errors.IsNotFound(err) {
			return false
		} else {
			Expect(err).Should(BeNil())
		}
		return oj.Status.Progress == appsv1alpha1.OperationProgressPending
	}, timeout, time.Second).Should(BeTrue())
}

func assertJobProgressProcessing(oj *appsv1alpha1.OperationJob, timeout time.Duration) {
	Eventually(func() bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
		if errors.IsNotFound(err) {
			return false
		} else {
			Expect(err).Should(BeNil())
		}
		return oj.Status.Progress == appsv1alpha1.OperationProgressProcessing
	}, timeout, time.Second).Should(BeTrue())
}

func assertJobProgressFailed(oj *appsv1alpha1.OperationJob, timeout time.Duration) {
	Eventually(func() bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
		if errors.IsNotFound(err) {
			return false
		} else {
			Expect(err).Should(BeNil())
		}
		return oj.Status.Progress == appsv1alpha1.OperationProgressFailed
	}, timeout, time.Second).Should(BeTrue())
}

func assertJobProgressSucceeded(oj *appsv1alpha1.OperationJob, timeout time.Duration) {
	Eventually(func() bool {
		err := c.Get(ctx, types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
		if errors.IsNotFound(err) {
			return false
		} else {
			Expect(err).Should(BeNil())
		}
		return oj.Status.Progress == appsv1alpha1.OperationProgressSucceeded
	}, timeout, time.Second).Should(BeTrue())
}

func updateOperationJobWithRetry(namespace, name string, updateFn func(*appsv1alpha1.OperationJob) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		operationJob := &appsv1alpha1.OperationJob{}
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, operationJob); err != nil {
			return err
		}

		if !updateFn(operationJob) {
			return nil
		}

		return c.Update(ctx, operationJob)
	})
}

func updatePodWithRetry(namespace, name string, updateFn func(*corev1.Pod) bool) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pod := &corev1.Pod{}
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			return err
		}

		if !updateFn(pod) {
			return nil
		}

		return c.Update(ctx, pod)
	})
}

func getPodNamesFromCollaSet(cs *appsv1alpha1.CollaSet) (names []string) {
	podList := &corev1.PodList{}
	Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
	for i, _ := range podList.Items {
		names = append(names, podList.Items[i].Name)
	}
	return
}

func createCollaSetWithReplicas(name string, namespace string, replicas int) *appsv1alpha1.CollaSet {
	cs := &appsv1alpha1.CollaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1alpha1.CollaSetSpec{
			Replicas: int32Pointer(int32(replicas)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
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
		return len(podList.Items) == replicas
	}, 5*time.Second, 1*time.Second).Should(BeTrue())

	Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)).Should(BeNil())
	Expect(expectedStatusReplicas(c, cs, 0, 0, 0, int32(replicas), int32(replicas), 0, 0, 0)).Should(BeNil())
	return cs
}

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

func TestOperationJobController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OperationJobController Test Suite")
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

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases"), filepath.Join("..", "..", "..", "config", "crd", "externals")},
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

	// operationJob controller
	r, request = testReconcile(NewReconciler(mgr))
	RegisterOperationJobActions()
	err = AddToMgr(mgr, r)
	Expect(err).NotTo(HaveOccurred())
	// collaset controller
	r, request = testReconcile(collaset.NewReconciler(mgr))
	err = collaset.AddToMgr(mgr, r)
	Expect(err).NotTo(HaveOccurred())
	// poddeletion controller
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
