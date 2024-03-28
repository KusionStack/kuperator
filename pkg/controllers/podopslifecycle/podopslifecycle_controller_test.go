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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/klogr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule/checker"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	"kusionstack.io/operating/pkg/utils/mixin"
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
	By("Tearing down the test environment")

	cancel()

	err := env.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("PodOpsLifecycle controller", func() {
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
		name      = "test"
		namespace = "default"
		id        = "123"
		timestamp = "1402144848"
	)

	AfterEach(func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		mgr.GetClient().Delete(context.Background(), pod)

		for {
			if len(request) == 0 {
				break
			}
			<-request
		}
	})

	It("Create a pod controlled by kusionstack", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{v1alpha1.ControlledByKusionStackLabelKey: "true"},
			},
			Spec: podSpec,
		}
		err := mgr.GetClient().Create(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		<-request

		// Not found the pod
		pod1 := &corev1.Pod{}
		name1 := name + "1"
		err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
			Name:      name1,
			Namespace: namespace,
		}, pod1)
		Expect(errors.IsNotFound(err)).To(Equal(true))

		_, err = podOpsLifecycle.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: name1, Namespace: namespace}})
		Expect(err).NotTo(HaveOccurred())

		// Found the pod
		Eventually(func() error {
			pod = &corev1.Pod{}
			err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      name,
				Namespace: namespace,
			}, pod)

			if len(pod.Status.Conditions) != 1 {
				return fmt.Errorf("expected 1 condition, got %d", len(pod.Status.Conditions))
			}
			if string(pod.Status.Conditions[0].Type) != v1alpha1.ReadinessGatePodServiceReady {
				return fmt.Errorf("expected type %s, got %s", v1alpha1.ReadinessGatePodServiceReady, pod.Status.Conditions[0].Type)
			}
			if pod.Status.Conditions[0].Status != corev1.ConditionTrue {
				return fmt.Errorf("expected status %s, got %s", corev1.ConditionFalse, pod.Status.Conditions[0].Status)
			}
			return nil
		}, 3*time.Second, 200*time.Millisecond).Should(BeNil())
	})

	It("Create pod with label preparing", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
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

		<-request

		Eventually(func() error {
			pod := &corev1.Pod{}
			if err := mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      name,
				Namespace: namespace,
			}, pod); err != nil {
				return fmt.Errorf("fail to get pod: %s", err)
			}

			// Need set readiness gate to false
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
		}, 3*time.Second, 200*time.Millisecond).Should(BeNil())
	})

	It("Create pod with label completing", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
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

		Eventually(func() error {
			pod := &corev1.Pod{}
			if err := mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      name,
				Namespace: namespace,
			}, pod); err != nil {
				return fmt.Errorf("fail to get pod: %s", err)
			}

			// Need set readiness gate to true
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
		}, 3*time.Second, 200*time.Millisecond).Should(BeNil())
	})

	It("Update pod with label compling", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{v1alpha1.ControlledByKusionStackLabelKey: "true"},
			},
			Spec: podSpec,
		}
		err := mgr.GetClient().Create(context.Background(), pod)
		Expect(err).NotTo(HaveOccurred())

		<-request

		Eventually(func() error {
			pod := &corev1.Pod{}
			err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      name,
				Namespace: namespace,
			}, pod)
			if err != nil {
				return fmt.Errorf("fail to get pod: %v", err)
			}

			pod.ObjectMeta.Labels = map[string]string{
				v1alpha1.ControlledByKusionStackLabelKey:                    "true",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, id):    timestamp,
				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, id): timestamp,
			}
			return mgr.GetClient().Update(context.Background(), pod)
		}, 3*time.Second, 200*time.Millisecond).Should(BeNil())

		Eventually(func() error {
			pod := &corev1.Pod{}
			if err := mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{
				Name:      name,
				Namespace: namespace,
			}, pod); err != nil {
				return fmt.Errorf("fail to get pod: %v", err)
			}

			// Need set readiness gate to true
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
		}, 3*time.Second, 200*time.Millisecond).Should(BeNil())
	})
})

var _ = Describe("Stage processing", func() {
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
		name      = "test"
		namespace = "default"
	)

	It("Process pre-check stage", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
					fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "abc",

					fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144849",
					fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "def",
				},
			},
			Spec: podSpec,
		}

		idToLabelsMap, _, err := PodIDAndTypesMap(pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(idToLabelsMap)).To(Equal(2))
		fmt.Println(idToLabelsMap)

		labels, err := podOpsLifecycle.preCheckStage(pod, idToLabelsMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(labels)).To(Equal(4))

		for k, v := range idToLabelsMap {
			key := fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, v[v1alpha1.PodOperationTypeLabelPrefix])
			operationPermission, ok := labels[key]
			Expect(ok).To(Equal(true))
			Expect(operationPermission).NotTo(BeEmpty())

			key = fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, k)
			preChecked, ok := labels[key]
			Expect(ok).To(Equal(true))
			Expect(preChecked).NotTo(BeEmpty())
		}
	})

	It("Process post-check stage", func() {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"): "1402144848",

					fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"): "1402144849",
				},
			},
			Spec: podSpec,
		}

		idToLabelsMap, _, err := PodIDAndTypesMap(pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(idToLabelsMap)).To(Equal(2))
		fmt.Println(idToLabelsMap)

		labels, err := podOpsLifecycle.postCheckStage(pod, idToLabelsMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(labels)).To(Equal(2))

		for k := range idToLabelsMap {
			key := fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, k)
			preChecked, ok := labels[key]
			Expect(ok).To(Equal(true))
			Expect(preChecked).NotTo(BeEmpty())
		}
	})
})

var _ = Describe("Finalizer processing", func() {
	var (
		name      = "test"
		namespace = "default"

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
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels: map[string]string{
					"app": name,
				},
			},
			Spec: podSpec,
		}
	)

	AfterEach(func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
		}
		mgr.GetClient().Delete(context.Background(), svc)
	})

	It("Available condition is not dirty", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app": name,
				},
				Ports: []corev1.ServicePort{
					{
						Name: "http",
						Port: 80,
					},
				},
			},
		}
		err := mgr.GetClient().Create(context.Background(), svc)
		Expect(err).NotTo(HaveOccurred())

		key := fmt.Sprintf("%s/%s/%s", "Service", svc.GetNamespace(), svc.GetName())
		isAvailableConditionDirty, err := podOpsLifecycle.isAvailableConditionDirty(pod, key)
		Expect(err).NotTo(HaveOccurred())
		Expect(isAvailableConditionDirty).To(Equal(false))
	})

	It("Available condition is dirty", func() {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					"app1": name,
				},
				Ports: []corev1.ServicePort{
					{
						Name: "http",
						Port: 80,
					},
				},
			},
		}
		err := mgr.GetClient().Create(context.Background(), svc)
		Expect(err).NotTo(HaveOccurred())

		key := fmt.Sprintf("%s/%s/%s", "Service", svc.GetNamespace(), svc.GetName())
		isAvailableConditionDirty, err := podOpsLifecycle.isAvailableConditionDirty(pod, key)
		Expect(err).NotTo(HaveOccurred())
		Expect(isAvailableConditionDirty).To(Equal(true))
	})
})

var _ = Describe("Label service-available processing", func() {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	Expect(err).NotTo(HaveOccurred())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test1",
			Namespace: "default",
			Labels: map[string]string{
				v1alpha1.ControlledByKusionStackLabelKey: "true",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod).
		Build()

	podOpsLifecycle := &ReconcilePodOpsLifecycle{
		ReconcilerMixin: &mixin.ReconcilerMixin{
			Client: fakeClient,
			Logger: klogr.New().WithName(controllerName),
		},
		expectation: expectations.NewResourceVersionExpectation(),
	}

	It("Service is available", func() {
		_, err := podOpsLifecycle.Reconcile(context.Background(), reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test1",
				Namespace: "default",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		pod1 := &corev1.Pod{}
		fakeClient.Get(context.Background(), client.ObjectKey{
			Name:      "test1",
			Namespace: "default",
		}, pod1)
		Expect(pod1.Labels[v1alpha1.PodServiceAvailableLabel]).NotTo(BeEmpty())
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
