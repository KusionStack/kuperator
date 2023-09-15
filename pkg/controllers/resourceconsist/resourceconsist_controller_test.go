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

package resourceconsist

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"kusionstack.io/operating/apis"
	"kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils/inject"
)

var (
	env *envtest.Environment
	mgr manager.Manager

	ctx    context.Context
	cancel context.CancelFunc

	rc = &DemoResourceProviderClient{}
)

var _ = Describe("resource-consist-controller", func() {
	service := corev1.Service{
		ObjectMeta: v1.ObjectMeta{
			Name:      "resource-consist-ut-svc",
			Namespace: "default",
			Labels: map[string]string{
				v1alpha1.ControlledByKusionStackLabelKey: "true",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "tcp-80",
					Port:     80,
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"resource-consist-ut": "resource-consist-ut",
			},
		},
	}

	Context("clean finalizer added", func() {
		It("clean finalizer added if service not deleting", func() {
			err := mgr.GetClient().Create(context.Background(), &service)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() bool {
				service1 := corev1.Service{}
				Expect(mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, &service1)).Should(BeNil())
				for _, flz := range service1.GetFinalizers() {
					if flz == cleanFinalizerPrefix+service.GetName() {
						return true
					}
				}
				return false
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})
	})

	Context("employer synced", func() {
		rc.On("QueryVip", mock.Anything).Return(&DemoResourceVipOps{}, nil)
		rc.On("CreateVip", mock.Anything).Return(&DemoResourceVipOps{}, nil)
		rc.On("UpdateVip", mock.Anything).Return(&DemoResourceVipOps{}, nil)
		rc.On("DeleteVip", mock.Anything).Return(&DemoResourceVipOps{}, nil)
		It("employer created", func() {
			Eventually(func() bool {
				details, exist := demoResourceVipStatusInProvider.Load("demo-expect-employer-id")
				return exist && details.(DemoServiceDetails).RemoteVIP == "demo-remote-VIP" && details.(DemoServiceDetails).RemoteVIPQPS == 100
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("employer updated", func() {
			demoResourceVipStatusInProvider.Store("demo-expect-employer-id", DemoServiceDetails{
				RemoteVIP:    "demo-remote-VIP",
				RemoteVIPQPS: 200,
			})

			// trigger reconcile
			service1 := corev1.Service{}
			Expect(mgr.GetClient().Get(context.TODO(), types.NamespacedName{
				Name:      service.Name,
				Namespace: service.Namespace,
			}, &service1)).Should(BeNil())
			if service1.Labels == nil {
				service1.Labels = make(map[string]string)
			}
			service1.Labels["demo-controller-trigger-reconcile"] = fmt.Sprintf("%d", time.Now().Unix())
			Expect(mgr.GetClient().Update(context.TODO(), &service1)).Should(BeNil())

			Eventually(func() bool {
				details, exist := demoResourceVipStatusInProvider.Load("demo-expect-employer-id")
				return exist && details.(DemoServiceDetails).RemoteVIP == "demo-remote-VIP" && details.(DemoServiceDetails).RemoteVIPQPS == 100
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("employer deleted", func() {
			Eventually(func() bool {
				service1 := corev1.Service{}
				Expect(mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, &service1)).Should(BeNil())
				flzs := service1.GetFinalizers()
				flzs = append(flzs, "kusionstack.io/ut-block-finalizer")
				service1.SetFinalizers(flzs)
				return mgr.GetClient().Update(context.TODO(), &service1) == nil
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Expect(mgr.GetClient().Delete(context.TODO(), &service)).Should(BeNil())
			Eventually(func() bool {
				service1 := corev1.Service{}
				Expect(mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, &service1)).Should(BeNil())
				return !service1.GetDeletionTimestamp().IsZero()
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
			Eventually(func() bool {
				_, exist := demoResourceVipStatusInProvider.Load("demo-expect-employer-id")
				return !exist
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				service1 := corev1.Service{}
				Expect(mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, &service1)).Should(BeNil())
				var flzs []string
				for _, flz := range service1.GetFinalizers() {
					if flz == "kusionstack.io/ut-block-finalizer" {
						continue
					}
					flzs = append(flzs, flz)
				}
				service1.SetFinalizers(flzs)
				return mgr.GetClient().Update(context.TODO(), &service1) == nil
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				service1 := corev1.Service{}
				err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, &service1)
				return errors.IsNotFound(err)
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})
	})

	Context("employee synced", func() {
		rc.On("QueryRealServer", mock.Anything).Return(&DemoResourceRsOps{}, nil)
		rc.On("CreateRealServer", mock.Anything).Return(&DemoResourceRsOps{}, nil)
		rc.On("UpdateRealServer", mock.Anything).Return(&DemoResourceRsOps{}, nil)
		rc.On("DeleteRealServer", mock.Anything).Return(&DemoResourceRsOps{}, nil)

		svc := corev1.Service{
			ObjectMeta: v1.ObjectMeta{
				Name:      "resource-consist-ut-svc-1",
				Namespace: "default",
				Labels: map[string]string{
					v1alpha1.ControlledByKusionStackLabelKey: "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:     "tcp-80",
						Port:     80,
						Protocol: corev1.ProtocolTCP,
					},
				},
				Selector: map[string]string{
					"resource-consist-ut": "resource-consist-ut-1",
				},
			},
		}

		pod := corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      "resource-consist-ut-pod",
				Namespace: "default",
				Labels: map[string]string{
					v1alpha1.ControlledByKusionStackLabelKey: "true",
					"resource-consist-ut":                    "resource-consist-ut-1",
				},
			},
			Spec: corev1.PodSpec{
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
			},
		}

		It("employee synced, employer created", func() {
			Expect(mgr.GetClient().Create(context.Background(), &svc)).Should(BeNil())
			Eventually(func() bool {
				details, exist := demoResourceVipStatusInProvider.Load("demo-expect-employer-id")
				return exist && details.(DemoServiceDetails).RemoteVIP == "demo-remote-VIP" && details.(DemoServiceDetails).RemoteVIPQPS == 100
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("employee synced, employees created", func() {
			Expect(mgr.GetClient().Create(context.TODO(), &pod)).Should(BeNil())
			Eventually(func() bool {
				pod1 := corev1.Pod{}
				err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &pod1)
				if err != nil {
					return false
				}
				pod1.Status = corev1.PodStatus{
					PodIP: "1.2.3.4",
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   v1alpha1.ReadinessGatePodServiceReady,
							Status: corev1.ConditionTrue,
						},
					},
				}
				return mgr.GetClient().Status().Update(context.TODO(), &pod1) == nil
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				details, exist := demoResourceRsStatusInProvider.Load(pod.Name)
				return exist && details.(DemoPodStatus).GetEmployeeName() == pod.Name &&
					details.(DemoPodStatus).GetEmployeeStatuses().(PodEmployeeStatuses).ExtraStatus.(PodExtraStatus).TrafficWeight == 100 &&
					details.(DemoPodStatus).GetEmployeeStatuses().(PodEmployeeStatuses).ExtraStatus.(PodExtraStatus).TrafficOn == true
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				pod1 := corev1.Pod{}
				_ = mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &pod1)
				containsLifecycleFlz := false
				for _, flz := range pod1.GetFinalizers() {
					if flz == GenerateLifecycleFinalizer(svc.Name) {
						containsLifecycleFlz = true
						break
					}
				}
				return containsLifecycleFlz
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				service1 := corev1.Service{}
				err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      svc.Name,
					Namespace: svc.Namespace,
				}, &service1)
				if err != nil {
					return false
				}
				return service1.GetAnnotations()[expectedFinalizerAddedAnnoKey] == pod.Name
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("employee synced, employees updated", func() {
			Eventually(func() bool {
				pod1 := corev1.Pod{}
				err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &pod1)
				if err != nil {
					return false
				}
				pod1.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   v1alpha1.ReadinessGatePodServiceReady,
						Status: corev1.ConditionFalse,
					},
				}
				return mgr.GetClient().Status().Update(context.TODO(), &pod1) == nil
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				details, exist := demoResourceRsStatusInProvider.Load(pod.Name)
				return exist && details.(DemoPodStatus).GetEmployeeName() == pod.Name &&
					details.(DemoPodStatus).GetEmployeeStatuses().(PodEmployeeStatuses).ExtraStatus.(PodExtraStatus).TrafficWeight == 0 &&
					details.(DemoPodStatus).GetEmployeeStatuses().(PodEmployeeStatuses).ExtraStatus.(PodExtraStatus).TrafficOn == false
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				pod1 := corev1.Pod{}
				err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &pod1)
				if err != nil {
					return false
				}
				pod1.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   v1alpha1.ReadinessGatePodServiceReady,
						Status: corev1.ConditionTrue,
					},
				}
				return mgr.GetClient().Status().Update(context.TODO(), &pod1) == nil
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				details, exist := demoResourceRsStatusInProvider.Load(pod.Name)
				return exist && details.(DemoPodStatus).GetEmployeeName() == pod.Name &&
					details.(DemoPodStatus).GetEmployeeStatuses().(PodEmployeeStatuses).ExtraStatus.(PodExtraStatus).TrafficWeight == 100 &&
					details.(DemoPodStatus).GetEmployeeStatuses().(PodEmployeeStatuses).ExtraStatus.(PodExtraStatus).TrafficOn == true
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

		It("employee synced, employees deleted", func() {
			pod1 := corev1.Pod{}
			Expect(mgr.GetClient().Get(context.TODO(), types.NamespacedName{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			}, &pod1)).Should(BeNil())

			Expect(mgr.GetClient().Delete(context.TODO(), &pod1)).Should(BeNil())

			Eventually(func() bool {
				pod1 := corev1.Pod{}
				err := mgr.GetClient().Get(context.TODO(), types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &pod1)
				return errors.IsNotFound(err)
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())

			Eventually(func() bool {
				_, exist := demoResourceRsStatusInProvider.Load(pod.Name)
				return !exist
			}, 3*time.Second, 100*time.Millisecond).Should(BeTrue())
		})

	})
})

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

	err = AddToMgr(mgr, NewDemoReconcileAdapter(mgr.GetClient(), rc))
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

func TestResourceConsistController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "resource consist controller test")
}
