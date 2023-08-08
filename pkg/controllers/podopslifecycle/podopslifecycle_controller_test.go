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

package podopslifecycle

import (
	"context"
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

var (
	env     *envtest.Environment
	mgr     manager.Manager
	request chan reconcile.Request

	ctx    context.Context
	cancel context.CancelFunc
)

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	schema := runtime.NewScheme()
	err := appsv1.SchemeBuilder.AddToScheme(schema) // deployment
	Expect(err).NotTo(HaveOccurred())

	env = &envtest.Environment{
		Scheme: schema,
	}
	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())

	mgr, err = manager.New(config, manager.Options{
		MetricsBindAddress: "0",
	})
	Expect(err).NotTo(HaveOccurred())

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

var _ = Describe("podopslifecycle controller", func() {
	var (
		podSpec = corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "nginx:latest",
				},
			},
			ReadinessGates: []corev1.PodReadinessGate{
				{
					ConditionType: v1alpha1.ReadinessGatePodServiceReady,
				},
			},
		}
		id   = "123"
		time = "1402144848"
	)

	AfterEach(func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}
		err := mgr.GetClient().Delete(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		for {
			if len(request) == 0 {
				break
			}
			<-request
		}
	})

	It("create pod with label complete", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Labels: map[string]string{
					fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, id): time,
				},
			},
			Spec: podSpec,
		}
		err := mgr.GetClient().Create(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		<-request

		pod = &corev1.Pod{}
		err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
			Name:      "test",
			Namespace: "default",
		}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Status.Conditions).To(HaveLen(1))
		Expect(string(pod.Status.Conditions[0].Type)).To(Equal(v1alpha1.ReadinessGatePodServiceReady))
		Expect(pod.Status.Conditions[0].Status).To(Equal(corev1.ConditionTrue))
	})

	It("update pod with label complete", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: podSpec,
		}
		err := mgr.GetClient().Create(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		<-request

		pod = &corev1.Pod{}
		err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
			Name:      "test",
			Namespace: "default",
		}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Status.Conditions).To(HaveLen(0))

		pod.ObjectMeta.Labels = map[string]string{
			fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, id): time,
		}
		err = mgr.GetClient().Update(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		<-request

		pod = &corev1.Pod{}
		err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
			Name:      "test",
			Namespace: "default",
		}, pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(pod.Status.Conditions).To(HaveLen(1))
		Expect(string(pod.Status.Conditions[0].Type)).To(Equal(v1alpha1.ReadinessGatePodServiceReady))
		Expect(pod.Status.Conditions[0].Status).To(Equal(corev1.ConditionTrue))
	})
})

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

func TestPodOpsLifecycleController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "podopslifecycle controller suite test")
}
