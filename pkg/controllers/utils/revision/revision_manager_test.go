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

package revision

import (
	"context"
	"encoding/json"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kusionstack.io/operating/apis"
	"kusionstack.io/operating/pkg/utils/inject"
)

var (
	env     *envtest.Environment
	mgr     manager.Manager
	request chan reconcile.Request

	ctx    context.Context
	cancel context.CancelFunc
	c      client.Client
)

var _ = Describe("revision manager", func() {

	selectedLabels := map[string]string{"test": "foo"}
	selector, err := metav1.ParseToLabelSelector("test=foo")
	Expect(err).Should(BeNil())

	It("revision construction", func() {
		testcase := "test-revision-construction"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      testcase,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: selectedLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		Expect(mgr.GetClient().Create(context.TODO(), deploy)).Should(BeNil())

		adapter := &OwnerAdapterImpl{
			name:            testcase,
			selector:        selector,
			selectedLabels:  selectedLabels,
			collisionCount:  0,
			historyLimit:    2,
			currentRevision: "test",
			inUsed:          true,
		}

		revisionManager := NewRevisionManager(mgr.GetClient(), mgr.GetScheme(), adapter)
		currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err := revisionManager.ConstructRevisions(deploy, false)
		Expect(err).Should(BeNil())
		Expect(createNewRevision).Should(BeTrue())
		Expect(collisionCount).ShouldNot(BeNil())
		Expect(*collisionCount).Should(BeEquivalentTo(0))
		Expect(len(revisionList)).Should(BeEquivalentTo(1))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(updatedRevision.Name).Should(BeEquivalentTo(currentRevision.Name))
		v1RevisionName := updatedRevision.Name
		waitingCacheUpdate(deploy.Namespace, 1)
		adapter.currentRevision = updatedRevision.Name

		// updating deploy spec should construct a new updated CcontrollerRevision
		deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
		currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err = revisionManager.ConstructRevisions(deploy, false)
		Expect(err).Should(BeNil())
		Expect(createNewRevision).Should(BeTrue())
		Expect(collisionCount).ShouldNot(BeNil())
		Expect(*collisionCount).Should(BeEquivalentTo(0))
		Expect(len(revisionList)).Should(BeEquivalentTo(2))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(updatedRevision.Name).ShouldNot(BeEquivalentTo(currentRevision.Name))
		waitingCacheUpdate(deploy.Namespace, 2)

		// reconcile with same spec with current revision is updated to updated revision
		adapter.currentRevision = updatedRevision.Name
		currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err = revisionManager.ConstructRevisions(deploy, false)
		Expect(err).Should(BeNil())
		Expect(createNewRevision).Should(BeFalse())
		Expect(collisionCount).ShouldNot(BeNil())
		Expect(*collisionCount).Should(BeEquivalentTo(0))
		Expect(len(revisionList)).Should(BeEquivalentTo(2))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(updatedRevision.Name).Should(BeEquivalentTo(currentRevision.Name))
		waitingCacheUpdate(deploy.Namespace, 2)

		// updating deploy spec to old version should not construct a new updated CcontrollerRevision
		deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v1"
		currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err = revisionManager.ConstructRevisions(deploy, false)
		Expect(err).Should(BeNil())
		Expect(createNewRevision).Should(BeFalse())
		Expect(collisionCount).ShouldNot(BeNil())
		Expect(*collisionCount).Should(BeEquivalentTo(0))
		Expect(len(revisionList)).Should(BeEquivalentTo(2))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
		Expect(updatedRevision.Name).ShouldNot(BeEquivalentTo(currentRevision.Name))
		Expect(updatedRevision.Name).Should(BeEquivalentTo(v1RevisionName))
	})

	It("revision cleanup", func() {
		testcase := "test-revision-cleanup"
		Expect(createNamespace(c, testcase)).Should(BeNil())

		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testcase,
				Name:      testcase,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: selector,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: selectedLabels,
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}
		Expect(mgr.GetClient().Create(context.TODO(), deploy)).Should(BeNil())

		adapter := &OwnerAdapterImpl{
			name:            testcase,
			selector:        selector,
			selectedLabels:  selectedLabels,
			collisionCount:  0,
			historyLimit:    0, // we at lease reserve 2 extra controller revisions
			currentRevision: "test",
			inUsed:          false,
		}

		revisionManager := NewRevisionManager(mgr.GetClient(), mgr.GetScheme(), adapter)
		revisionManager.ConstructRevisions(deploy, false)
		waitingCacheUpdate(deploy.Namespace, 1)

		deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
		revisionManager.ConstructRevisions(deploy, false)
		waitingCacheUpdate(deploy.Namespace, 2)

		deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v3"
		revisionManager.ConstructRevisions(deploy, false)
		waitingCacheUpdate(deploy.Namespace, 3)

		revisionManager.ConstructRevisions(deploy, false)
		waitingCacheUpdate(deploy.Namespace, 2)
	})

})

func waitingCacheUpdate(namespace string, expectedRevisionCount int) {
	Eventually(func() error {
		revisionList := &appsv1.ControllerRevisionList{}
		if err := c.List(context.TODO(), revisionList, &client.ListOptions{Namespace: namespace}); err != nil {
			return err
		}

		if len(revisionList.Items) != expectedRevisionCount {
			return fmt.Errorf("expected %d, got %d\n", expectedRevisionCount, len(revisionList.Items))
		}

		return nil
	}, 5*time.Second, 1*time.Second).Should(BeNil())
}

type OwnerAdapterImpl struct {
	name            string
	selector        *metav1.LabelSelector
	selectedLabels  map[string]string
	collisionCount  int32
	historyLimit    int32
	currentRevision string
	inUsed          bool
}

func (a OwnerAdapterImpl) GetSelector(obj metav1.Object) *metav1.LabelSelector {
	return a.selector
}

func (a OwnerAdapterImpl) GetCollisionCount(obj metav1.Object) *int32 {
	return &a.collisionCount
}

func (a OwnerAdapterImpl) GetHistoryLimit(obj metav1.Object) int32 {
	return a.historyLimit
}

func (a OwnerAdapterImpl) GetPatch(obj metav1.Object) ([]byte, error) {
	// mock patch
	return json.Marshal(obj)
}

func (a OwnerAdapterImpl) GetSelectorLabels(obj metav1.Object) map[string]string {
	return a.selectedLabels
}

func (a OwnerAdapterImpl) GetCurrentRevision(obj metav1.Object) string {
	return a.currentRevision
}

func (a OwnerAdapterImpl) IsInUsed(obj metav1.Object, controllerRevision string) bool {
	return a.inUsed
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
	crList := &appsv1.ControllerRevisionList{}
	Expect(mgr.GetClient().List(context.Background(), crList)).Should(BeNil())

	for i := range crList.Items {
		Expect(mgr.GetClient().Delete(context.TODO(), &crList.Items[i])).Should(BeNil())
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
