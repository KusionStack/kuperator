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

package poddeletion

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
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

var _ = Describe("Pod Deletion controller", func() {

	It("deletion reconcile", func() {
		testcase := "delete-pod"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "test",
				Labels: map[string]string{
					"kusionstack.io/control": "true",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test",
						Image: "nginx:v1",
					},
				},
			},
		}

		Expect(c.Create(context.TODO(), pod)).Should(BeNil())
		Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)
		}, 5*time.Second, 1*time.Second).Should(BeNil())

		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			pod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey] = "true"
			return true
		})).Should(BeNil())

		time.After(3 * time.Second)
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)).Should(BeNil())

		// allow Pod to update
		Expect(updatePodWithRetry(c, pod.Namespace, pod.Name, func(pod *corev1.Pod) bool {
			labelOperate := fmt.Sprintf("%s/%s", appsv1alpha1.PodOperateLabelPrefix, OpsLifecycleAdapter.GetID())
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[labelOperate] = "true"
			// mock pod is replaced deleting
			pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = fmt.Sprintf("%d", time.Now().UnixNano())
			return true
		})).Should(BeNil())

		// pod should be deleted
		Eventually(func() error {
			return c.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)
		}, 5*time.Second, 1*time.Second).ShouldNot(BeNil())
	})
})

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

func TestPodDeletionController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PodDeletionController Test Suite")
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

	scheme := scheme.Scheme
	err = appsv1.SchemeBuilder.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())
	err = appsv1alpha1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	mgr, err = manager.New(config, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           inject.NewCacheWithFieldIndex,
	})
	Expect(err).NotTo(HaveOccurred())

	c = mgr.GetClient()

	var r reconcile.Reconciler
	r, request = testReconcile(NewReconciler(mgr))
	err = AddToMgr(mgr, r)
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
