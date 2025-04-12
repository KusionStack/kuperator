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

package poddecoration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/kube-utils/controller/history"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/anno"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/revision"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/strategy"
)

var (
	env         *envtest.Environment
	cancel      context.CancelFunc
	ctx         context.Context
	c           client.Client
	revisionMgr history.HistoryManager
)

const timeoutInterval = 5 * time.Second
const pollInterval = 500 * time.Millisecond

var _ = Describe("Test PodDecoration getter", func() {
	It("test getter", func() {
		testcase := "test-getter"
		Expect(createNamespace(testcase)).Should(BeNil())
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
				Weight: int32Pointer(10),
				UpdateStrategy: appsv1alpha1.PodDecorationUpdateStrategy{
					RollingUpdate: &appsv1alpha1.PodDecorationRollingUpdate{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								appsv1alpha1.PodInstanceIDLabelKey: "0",
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
			Status: appsv1alpha1.PodDecorationStatus{
				CurrentRevision: "",
				UpdatedRevision: "",
			},
		}
		Expect(c.Create(ctx, podDecoration)).ShouldNot(HaveOccurred())
		Eventually(func() error {
			return c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration)
		}, timeoutInterval, pollInterval).Should(BeNil())
		_, updatedRevisionV1, _, _, _, err := revisionMgr.ConstructRevisions(ctx, podDecoration)
		Expect(updatedRevisionV1).ShouldNot(BeNil())
		currentRevision := updatedRevisionV1.Name
		Expect(err).ShouldNot(HaveOccurred())
		Eventually(func() error {
			rev := &appsv1.ControllerRevision{}
			err := c.Get(ctx, types.NamespacedName{Name: updatedRevisionV1.Name, Namespace: testcase}, rev)
			if err != nil {
				fmt.Println(err)
			}
			return err
		}, timeoutInterval, pollInterval).Should(BeNil())
		podDecoration.Spec.Template.Containers[0].Image = "nginx:v1"
		Expect(c.Update(ctx, podDecoration)).Should(BeNil())
		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration); err == nil {
				return podDecoration.Spec.Template.Containers[0].Image == "nginx:v1"
			}
			return false
		}, timeoutInterval, pollInterval).Should(Equal(true))
		podDecoration.Status.CurrentRevision = currentRevision
		Expect(c.Status().Update(ctx, podDecoration)).Should(BeNil())
		Eventually(func() bool {
			if err := c.Get(ctx, types.NamespacedName{Name: podDecoration.Name, Namespace: testcase}, podDecoration); err == nil {
				return podDecoration.Status.CurrentRevision == currentRevision
			}
			return false
		}, timeoutInterval, pollInterval).Should(Equal(true))

		_, updatedRevisionV2, _, _, _, err := revisionMgr.ConstructRevisions(ctx, podDecoration)
		Expect(err).Should(BeNil())
		Expect(updatedRevisionV2).ShouldNot(BeNil())
		updatedRevision := updatedRevisionV2.Name
		podDecoration.Status.UpdatedRevision = updatedRevision
		Expect(strategy.SharedStrategyController.UpdateSelectedPods(ctx, podDecoration, nil)).Should(BeNil())
		strategy.SharedStrategyController.Synced()
		getter, err := NewPodDecorationGetter(c, testcase)
		tu := true
		po0 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "pod",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "CollaSet",
						Name:       "cs",
						Controller: &tu,
					},
				},
				Labels: map[string]string{
					"app":                              "foo",
					appsv1alpha1.PodInstanceIDLabelKey: strconv.Itoa(0),
				},
			},
		}
		anno.SetDecorationInfo(po0, map[string]*appsv1alpha1.PodDecoration{
			currentRevision: podDecoration,
		})
		po1 := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      "pod",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "CollaSet",
						Name:       "cs",
						Controller: &tu,
					},
				},
				Labels: map[string]string{
					"app":                              "foo",
					appsv1alpha1.PodInstanceIDLabelKey: strconv.Itoa(1),
				},
			},
		}
		pds, err := getter.GetEffective(ctx, po0)
		Expect(err).Should(BeNil())
		Expect(len(pds)).Should(Equal(1))
		Expect(pds[updatedRevision]).ShouldNot(BeNil())

		pds, err = getter.GetEffective(ctx, po1)
		Expect(err).Should(BeNil())
		Expect(len(pds)).Should(Equal(1))
		Expect(pds[currentRevision]).ShouldNot(BeNil())

		pds, err = getter.GetOnPod(ctx, po0)
		Expect(err).Should(BeNil())
		Expect(len(pds)).Should(Equal(1))
		Expect(pds[currentRevision]).ShouldNot(BeNil())

		pds, err = getter.GetByRevisions(ctx, currentRevision, updatedRevision)
		Expect(err).Should(BeNil())
		Expect(len(pds)).Should(Equal(2))

	})
})

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())
	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "config", "crd", "bases")},
	}
	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())
	sch := scheme.Scheme
	Expect(appsv1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	Expect(appsv1alpha1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	cl, err := client.New(config, client.Options{Scheme: sch})
	c = cl
	strategy.SharedStrategyController.InjectClient(c)
	Expect(err).NotTo(HaveOccurred())
	revisionMgr = history.NewHistoryManager(history.NewRevisionControl(cl, cl), &revision.RevisionOwnerAdapter{})
})

var _ = AfterEach(func() {

})

var _ = AfterSuite(func() {
	env.Stop()
})

func createNamespace(namespaceName string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespaceName,
		},
	}
	return c.Create(context.TODO(), ns)
}

func getPodDecorationPatch(pd *appsv1alpha1.PodDecoration) ([]byte, error) {
	dsBytes, err := json.Marshal(pd)
	if err != nil {
		return nil, err
	}
	var raw map[string]interface{}
	err = json.Unmarshal(dsBytes, &raw)
	if err != nil {
		return nil, err
	}
	objCopy := make(map[string]interface{})
	specCopy := make(map[string]interface{})

	spec := raw["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	weight := spec["weight"]

	template["$patch"] = "replace"
	specCopy["template"] = template
	specCopy["weight"] = weight
	objCopy["spec"] = specCopy
	patch, err := json.Marshal(objCopy)
	return patch, err
}

func int32Pointer(val int32) *int32 {
	return &val
}
