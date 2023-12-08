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
	"time"

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

	"kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule/checker"
)

var (
	env             *envtest.Environment
	podOpsLifecycle *ReconcilePodOpsLifecycle
	mgr             manager.Manager
	request         chan reconcile.Request

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

	podOpsLifecycle = NewReconciler(mgr)
	podOpsLifecycle.podTransitionRuleManager = &mockPodTransitionRuleManager{}

	var r reconcile.Reconciler
	r, request = testReconcile(podOpsLifecycle)
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
		id        = "123"
		timestamp = "1402144848"
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

	It("update pod with stage pre-check", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.ControlledByKusionStackLabelKey: "true"},
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

	It("create pod with label prepare", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Labels: map[string]string{
					v1alpha1.ControlledByKusionStackLabelKey:                   "true",
					fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, id): timestamp,
					fmt.Sprintf("%s/%s", v1alpha1.PodPreparingLabelPrefix, id): timestamp,
				},
			},
			Spec: podSpec,
		}
		err := mgr.GetClient().Create(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		pod = &corev1.Pod{}
		Eventually(func() error {
			if err := mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      "test",
				Namespace: "default",
			}, pod); err != nil {
				return fmt.Errorf("fail to get pod: %s", err)
			}

			if len(pod.Status.Conditions) != 1 {
				return fmt.Errorf("expected 1 condition, got %d", len(pod.Status.Conditions))
			}

			if string(pod.Status.Conditions[0].Type) != v1alpha1.ReadinessGatePodServiceReady {
				return fmt.Errorf("expected type %s, got %s", v1alpha1.ReadinessGatePodServiceReady, pod.Status.Conditions[0].Type)
			}

			if pod.Status.Conditions[0].Status != corev1.ConditionFalse {
				return fmt.Errorf("expected status %s, got %s", corev1.ConditionFalse, pod.Status.Conditions[0].Status)
			}

			return nil
		}, 5*time.Second, 1*time.Second).Should(BeNil())
	})

	It("create pod with label complete", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Labels: map[string]string{
					v1alpha1.ControlledByKusionStackLabelKey:                    "true",
					fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, id):    timestamp,
					fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, id): timestamp,
				},
			},
			Spec: podSpec,
		}
		err := mgr.GetClient().Create(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		<-request

		pod = &corev1.Pod{}
		Eventually(func() error {
			if err := mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      "test",
				Namespace: "default",
			}, pod); err != nil {
				return fmt.Errorf("fail to get pod: %s", err)
			}

			if len(pod.Status.Conditions) != 1 {
				return fmt.Errorf("expected 1 condition, got %d", len(pod.Status.Conditions))
			}

			if string(pod.Status.Conditions[0].Type) != v1alpha1.ReadinessGatePodServiceReady {
				return fmt.Errorf("expected type %s, got %s", v1alpha1.ReadinessGatePodServiceReady, pod.Status.Conditions[0].Type)
			}

			if pod.Status.Conditions[0].Status != corev1.ConditionTrue {
				return fmt.Errorf("expected status %s, got %s", corev1.ConditionTrue, pod.Status.Conditions[0].Status)
			}

			return nil
		}, 5*time.Second, 1*time.Second).Should(BeNil())
	})

	It("update pod with label complete", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Labels:    map[string]string{v1alpha1.ControlledByKusionStackLabelKey: "true"},
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

		pod.ObjectMeta.Labels = map[string]string{
			v1alpha1.ControlledByKusionStackLabelKey:                    "true",
			fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, id):    timestamp,
			fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, id): timestamp,
		}
		err = mgr.GetClient().Update(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		pod = &corev1.Pod{}
		Eventually(func() error {
			if err := mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      "test",
				Namespace: "default",
			}, pod); err != nil {
				return fmt.Errorf("fail to get pod: %s", err)
			}

			if len(pod.Status.Conditions) != 1 {
				return fmt.Errorf("expected 1 condition, got %d", len(pod.Status.Conditions))
			}

			if string(pod.Status.Conditions[0].Type) != v1alpha1.ReadinessGatePodServiceReady {
				return fmt.Errorf("expected type %s, got %s", v1alpha1.ReadinessGatePodServiceReady, pod.Status.Conditions[0].Type)
			}

			if pod.Status.Conditions[0].Status != corev1.ConditionTrue {
				return fmt.Errorf("expected status %s, got %s", corev1.ConditionTrue, pod.Status.Conditions[0].Status)
			}

			return nil
		}, 5*time.Second, 1*time.Second).Should(BeNil())
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

var _ podtransitionrule.ManagerInterface = &mockPodTransitionRuleManager{}

type mockPodTransitionRuleManager struct {
	*checker.CheckState
}

func (rsm *mockPodTransitionRuleManager) RegisterStage(key string, inStage func(obj client.Object) bool) {
}

func (rsm *mockPodTransitionRuleManager) RegisterCondition(opsCondition string, inCondition func(obj client.Object) bool) {
}

func (rsm *mockPodTransitionRuleManager) SetupPodTransitionRuleController(manager.Manager) error {
	return nil
}

func (rsm *mockPodTransitionRuleManager) GetState(context.Context, client.Client, client.Object) (checker.CheckState, error) {
	if rsm.CheckState == nil {
		return checker.CheckState{}, nil
	}
	return *rsm.CheckState, nil
}

func TestControlledByPodOpsLifecycleler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "podopslifecycle controller suite test")
}
