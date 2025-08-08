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

package strategy

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	env    *envtest.Environment
	cancel context.CancelFunc
	ctx    context.Context
	c      client.Client
)

const (
	timeoutInterval = 5 * time.Second
	pollInterval    = 500 * time.Millisecond
)

var _ = Describe("Test PodDecoration strategy manager", func() {
	It("RollingUpdate by Selector", func() {
		testcase := "test-pd-0"
		Expect(createNamespace(testcase)).Should(BeNil())
		_, pods, err := createCollaSetResources(testcase, "colla-a", 2)
		Expect(err).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			return len(podList.Items)
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(2))

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
				Weight: int32Pointer(1),
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
		// Create pd
		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, timeoutInterval, pollInterval).Should(BeNil())
		podDecoration.Status.ObservedGeneration = podDecoration.Generation
		podDecoration.Status.CurrentRevision = "1"
		podDecoration.Status.UpdatedRevision = "2"
		Expect(c.Status().Update(ctx, podDecoration)).Should(BeNil())
		SharedStrategyController.(*strategyManager).synced = false
		Expect(SharedStrategyController.Start(ctx, nil, nil)).NotTo(HaveOccurred())
		Expect(SharedStrategyController.WaitForSync(ctx)).Should(BeNil())
		Expect(len(SharedStrategyController.LatestPodDecorations(testcase))).Should(Equal(1))
		updatedRevisions, stableRevisions := SharedStrategyController.EffectivePodRevisions(pods[0])
		Expect(len(updatedRevisions)).Should(Equal(0))
		Expect(len(stableRevisions)).Should(Equal(1))
		Expect(SharedStrategyController.UpdateSelectedPods(ctx, podDecoration, []*corev1.Pod{pods[1]})).Should(BeNil())
		updatedRevisions, stableRevisions = SharedStrategyController.EffectivePodRevisions(pods[0])
		Expect(len(updatedRevisions)).Should(Equal(0))
		Expect(len(stableRevisions)).Should(Equal(1))
	})

	It("Partition pods sorter", func() {
		testcase := "test-pd-1"
		replicas := 3
		Expect(createNamespace(testcase)).Should(BeNil())
		_, pods, err := createCollaSetResources(testcase, "colla-a", replicas)
		Expect(err).ShouldNot(HaveOccurred())
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
				Weight: int32Pointer(1),
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
		getter := newPodRelatedResourceGetter(ctx, c)
		var infos []*podInfo
		for i := 0; i < replicas; i++ {
			info, err := getter.buildPodInfo(pods[i], podDecoration)
			Expect(err).NotTo(HaveOccurred())
			Expect(info).ShouldNot(BeNil())
			infos = append(infos, info)
		}
		sortedInfos := &sortedPodInfo{
			infos:    infos,
			revision: "",
		}
		sort.Sort(sortedInfos)
		Expect(sortedInfos.infos[0].instanceId).Should(Equal("1"))
		Expect(sortedInfos.infos[1].instanceId).Should(Equal("2"))
		Expect(sortedInfos.infos[2].instanceId).Should(Equal("3"))

		pods[1].Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name] = "1"
		pods[2].Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name] = "2"
		getter = newPodRelatedResourceGetter(ctx, c)
		for i := 0; i < replicas; i++ {
			info, err := getter.buildPodInfo(pods[i], podDecoration)
			Expect(err).NotTo(HaveOccurred())
			Expect(info).ShouldNot(BeNil())
			infos = append(infos, info)
		}
		sortedInfos = &sortedPodInfo{
			infos:    infos,
			revision: "2",
		}
		sort.Sort(sortedInfos)
		Expect(sortedInfos.infos[0].instanceId).Should(Equal("3"))
		Expect(sortedInfos.infos[1].instanceId).Should(Equal("2"))
		Expect(sortedInfos.infos[2].instanceId).Should(Equal("1"))
	})

	It("RollingUpdate by Partition", func() {
		testcase := "test-pd-2"
		Expect(createNamespace(testcase)).Should(BeNil())
		_, pods, err := createCollaSetResources(testcase, "colla-a", 3)
		Expect(err).Should(BeNil())
		podList := &corev1.PodList{}
		Eventually(func() int {
			Expect(c.List(ctx, podList, client.InNamespace(testcase))).Should(BeNil())
			return len(podList.Items)
		}, timeoutInterval, pollInterval).Should(BeEquivalentTo(3))

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
				Weight: int32Pointer(1),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Partition: int32Pointer(2),
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
		// Create pd
		Expect(c.Create(ctx, podDecoration)).Should(BeNil())
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, timeoutInterval, pollInterval).Should(BeNil())
		podDecoration.Status.ObservedGeneration = podDecoration.Generation
		podDecoration.Status.CurrentRevision = "1"
		podDecoration.Status.UpdatedRevision = "2"
		Expect(c.Status().Update(ctx, podDecoration)).Should(BeNil())
		SharedStrategyController.(*strategyManager).synced = false
		Expect(SharedStrategyController.Start(ctx, nil, nil)).NotTo(HaveOccurred())
		Expect(SharedStrategyController.WaitForSync(ctx)).Should(BeNil())
		Expect(len(SharedStrategyController.LatestPodDecorations(testcase))).Should(Equal(1))
		// order: [pod-1], [pod-2, pod-3]
		updatedRevisions, stableRevisions := SharedStrategyController.EffectivePodRevisions(pods[0])
		Expect(len(updatedRevisions)).Should(Equal(1))
		Expect(len(stableRevisions)).Should(Equal(0))
		updatedRevisions, stableRevisions = SharedStrategyController.EffectivePodRevisions(pods[1])
		Expect(len(updatedRevisions)).Should(Equal(0))
		Expect(len(stableRevisions)).Should(Equal(1))
		updatedRevisions, stableRevisions = SharedStrategyController.EffectivePodRevisions(pods[2])
		Expect(len(updatedRevisions)).Should(Equal(0))
		Expect(len(stableRevisions)).Should(Equal(1))

		pods[1].Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name] = "1"
		pods[2].Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecoration.Name] = "2"
		// order: [pod-3], [pod-2, pod-1]
		Expect(SharedStrategyController.UpdateSelectedPods(ctx, podDecoration, pods)).Should(BeNil())
		updatedRevisions, stableRevisions = SharedStrategyController.EffectivePodRevisions(pods[0])
		Expect(len(updatedRevisions)).Should(Equal(0))
		Expect(len(stableRevisions)).Should(Equal(1))
		updatedRevisions, stableRevisions = SharedStrategyController.EffectivePodRevisions(pods[1])
		Expect(len(updatedRevisions)).Should(Equal(0))
		Expect(len(stableRevisions)).Should(Equal(1))
		updatedRevisions, stableRevisions = SharedStrategyController.EffectivePodRevisions(pods[2])
		Expect(len(updatedRevisions)).Should(Equal(1))
		Expect(len(stableRevisions)).Should(Equal(0))
		SharedStrategyController.DeletePodDecoration(podDecoration)
		Expect(len(SharedStrategyController.LatestPodDecorations(testcase))).Should(Equal(0))
	})
})

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())
	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases")},
	}
	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())
	sch := scheme.Scheme
	Expect(appsv1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	Expect(appsv1alpha1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	cl, err := client.New(config, client.Options{Scheme: sch})
	c = cl
	Expect(err).NotTo(HaveOccurred())
	SharedStrategyController.InjectClient(c)
})

var _ = AfterEach(func() {
	csList := &appsv1alpha1.CollaSetList{}
	Expect(c.List(context.Background(), csList)).Should(BeNil())

	for i := range csList.Items {
		Expect(c.Delete(context.TODO(), &csList.Items[i])).Should(BeNil())
	}
	pods := &corev1.PodList{}
	Expect(c.List(context.Background(), pods)).Should(BeNil())
	for i := range pods.Items {
		Expect(c.Delete(context.TODO(), &pods.Items[i])).Should(BeNil())
	}
	Eventually(func() int {
		Expect(c.List(context.Background(), pods)).Should(BeNil())
		return len(pods.Items)
	}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(0))

	pdList := &appsv1alpha1.PodDecorationList{}
	Expect(c.List(context.Background(), pdList)).Should(BeNil())

	for i := range pdList.Items {
		Expect(c.Delete(context.TODO(), &pdList.Items[i])).Should(BeNil())
	}
	Eventually(func() int {
		Expect(c.List(context.Background(), pdList)).Should(BeNil())
		return len(pdList.Items)
	}, 5*time.Second, 1*time.Second).Should(BeEquivalentTo(0))
	nsList := &corev1.NamespaceList{}
	Expect(c.List(context.Background(), nsList)).Should(BeNil())
	for i := range nsList.Items {
		if strings.HasPrefix(nsList.Items[i].Name, "test-") {
			c.Delete(context.TODO(), &nsList.Items[i])
		}
	}
})

var _ = AfterSuite(func() {
	cancel()
	env.Stop()
})

func TestPodDecorationStrategyController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test PodDecoration Strategy Controller")
}

func int32Pointer(val int32) *int32 {
	return &val
}

func createNamespace(namespaceName string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	return c.Create(context.TODO(), ns)
}

func createCollaSetResources(namespace, name string, replicas int) (*appsv1alpha1.CollaSet, []*corev1.Pod, error) {
	collaSet := &appsv1alpha1.CollaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1alpha1.CollaSetSpec{
			Replicas: int32Pointer(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
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
							Name:  name,
							Image: "nginx:v1",
						},
					},
				},
			},
		},
	}
	if err := c.Create(ctx, collaSet); err != nil {
		return nil, nil, err
	}
	if err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, collaSet); err != nil {
		return nil, nil, err
	}
	tu := true
	var pods []*corev1.Pod
	for i := 1; i <= replicas; i++ {
		po := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name + "-" + strconv.Itoa(i-1),
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "CollaSet",
						Name:       name,
						Controller: &tu,
						APIVersion: "apps.kusionstack.io/v1alpha1",
						UID:        collaSet.UID,
					},
				},
				Labels: map[string]string{
					"app":                                 "foo",
					"collaset.kusionstack.io/instance-id": strconv.Itoa(i),
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
		}
		if err := c.Create(ctx, po); err != nil {
			return nil, nil, err
		}
		pods = append(pods, po)
	}
	rc := &appsv1alpha1.ResourceContext{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: appsv1alpha1.ResourceContextSpec{
			Contexts: []appsv1alpha1.ContextDetail{
				{
					ID:   1,
					Data: map[string]string{},
				},
				{
					ID:   2,
					Data: map[string]string{},
				},
			},
		},
	}
	if err := c.Create(ctx, rc); err != nil {
		return nil, nil, err
	}
	return collaSet, pods, nil
}
