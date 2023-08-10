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

package resourcecontext

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"kusionstack.io/kafed/apis"
	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/collaset"
	collasetutils "kusionstack.io/kafed/pkg/controllers/collaset/utils"
	"kusionstack.io/kafed/pkg/controllers/poddeletion"
	"kusionstack.io/kafed/pkg/utils/inject"
)

var (
	env     *envtest.Environment
	mgr     manager.Manager
	request chan reconcile.Request

	ctx    context.Context
	cancel context.CancelFunc
	c      client.Client
)

var _ = Describe("ResourceContext controller", func() {

	It("resource context reconcile", func() {
		testcase := "test-reclaim"
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

		names := sets.NewString()
		for _, pod := range podList.Items {
			names.Insert(pod.Name)
			// mark Pod to delete
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = "true"
				return true
			})).Should(BeNil())
			// allow Pod to delete
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, poddeletion.OpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = "true"
				return true
			})).Should(BeNil())
		}

		// wait for all original Pods deleted
		Eventually(func() bool {
			Expect(c.List(context.TODO(), podList, client.InNamespace(cs.Namespace))).Should(BeNil())
			if len(podList.Items) != 2 {
				return false
			}

			for _, pod := range podList.Items {
				if pod.DeletionTimestamp != nil {
					// still in terminating
					return false
				}
			}

			for _, pod := range podList.Items {
				if names.Has(pod.Name) {
					return false
				}
			}

			return true
		}, 5*time.Second, 1*time.Second).Should(BeTrue())

		resourceContext := &appsv1alpha1.ResourceContext{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, resourceContext)).Should(BeNil())

		Expect(updateCollaSetWithRetry(c, cs.Namespace, cs.Name, func(cls *appsv1alpha1.CollaSet) bool {
			cls.Spec.Replicas = int32Pointer(0)
			return true
		})).Should(BeNil())

		for _, pod := range podList.Items {
			// allow Pod to scale in
			Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
				labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, collasetutils.ScaleInOpsLifecycleAdapter.GetID())
				pod.Labels[labelOperate] = "true"
				return true
			})).Should(BeNil())
		}

		Eventually(func() error {
			return expectedStatusReplicas(c, cs, 0, 0, 0, 0, 0, 0, 0, 0)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, resourceContext)
		}, 500*time.Second, 1*time.Second).ShouldNot(BeNil())
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
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
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
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
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

func TestResourceContextController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ResourceContext Test Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}
	env.ControlPlane.GetAPIServer().URL = &url.URL{
		Host: "127.0.0.1:10001",
	}
	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())

	mgr, err = manager.New(config, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           inject.NewCacheWithFieldIndex,
	})
	Expect(err).NotTo(HaveOccurred())

	scheme := mgr.GetScheme()
	err = appsv1.SchemeBuilder.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = apis.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	var r reconcile.Reconciler
	r, request = testReconcile(NewReconciler(mgr))
	err = AddToMgr(mgr, r)
	Expect(err).NotTo(HaveOccurred())

	r, request = testReconcile(collaset.NewReconciler(mgr))
	err = collaset.AddToMgr(mgr, r)
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
