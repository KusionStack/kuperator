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
	"testing"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	c client.Client
	g *WithT

	selectedLabels = map[string]string{"test": "foo"}
	selector, _    = metav1.ParseToLabelSelector("test=foo")
)

func TestRevisionConstruction(t *testing.T) {
	g = NewGomegaWithT(t)
	testcase := "test-revision-construction"
	schema := runtime.NewScheme()
	corev1.AddToScheme(schema)
	appsv1.AddToScheme(schema)
	c = fake.NewClientBuilder().WithScheme(schema).Build()
	g.Expect(createNamespace(c, testcase)).Should(BeNil())

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
	g.Expect(c.Create(context.TODO(), deploy)).Should(BeNil())

	adapter := &OwnerAdapterImpl{
		name:            testcase,
		selector:        selector,
		selectedLabels:  selectedLabels,
		collisionCount:  0,
		historyLimit:    2,
		currentRevision: "test",
		inUsed:          true,
	}

	revisionManager := NewRevisionManager(c, schema, adapter)
	currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err := revisionManager.ConstructRevisions(deploy, false)
	g.Expect(err).Should(BeNil())
	g.Expect(createNewRevision).Should(BeTrue())
	g.Expect(collisionCount).ShouldNot(BeNil())
	g.Expect(*collisionCount).Should(BeEquivalentTo(0))
	g.Expect(len(revisionList)).Should(BeEquivalentTo(1))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(updatedRevision.Name).Should(BeEquivalentTo(currentRevision.Name))
	v1RevisionName := updatedRevision.Name
	waitingCacheUpdate(deploy.Namespace, 1)
	adapter.currentRevision = updatedRevision.Name

	// updating deploy spec should construct a new updated CcontrollerRevision
	deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
	currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err = revisionManager.ConstructRevisions(deploy, false)
	g.Expect(err).Should(BeNil())
	g.Expect(createNewRevision).Should(BeTrue())
	g.Expect(collisionCount).ShouldNot(BeNil())
	g.Expect(*collisionCount).Should(BeEquivalentTo(0))
	g.Expect(len(revisionList)).Should(BeEquivalentTo(2))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(updatedRevision.Name).ShouldNot(BeEquivalentTo(currentRevision.Name))
	waitingCacheUpdate(deploy.Namespace, 2)

	// reconcile with same spec with current revision is updated to updated revision
	adapter.currentRevision = updatedRevision.Name
	currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err = revisionManager.ConstructRevisions(deploy, false)
	g.Expect(err).Should(BeNil())
	g.Expect(createNewRevision).Should(BeFalse())
	g.Expect(collisionCount).ShouldNot(BeNil())
	g.Expect(*collisionCount).Should(BeEquivalentTo(0))
	g.Expect(len(revisionList)).Should(BeEquivalentTo(2))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(updatedRevision.Name).Should(BeEquivalentTo(currentRevision.Name))
	waitingCacheUpdate(deploy.Namespace, 2)

	// updating deploy spec to old version should not construct a new updated CcontrollerRevision
	deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v1"
	currentRevision, updatedRevision, revisionList, collisionCount, createNewRevision, err = revisionManager.ConstructRevisions(deploy, false)
	g.Expect(err).Should(BeNil())
	g.Expect(createNewRevision).Should(BeFalse())
	g.Expect(collisionCount).ShouldNot(BeNil())
	g.Expect(*collisionCount).Should(BeEquivalentTo(0))
	g.Expect(len(revisionList)).Should(BeEquivalentTo(2))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(len(currentRevision.Name)).ShouldNot(BeEquivalentTo(0))
	g.Expect(updatedRevision.Name).ShouldNot(BeEquivalentTo(currentRevision.Name))
	g.Expect(updatedRevision.Name).Should(BeEquivalentTo(v1RevisionName))
}

func TestRevisionCleanUp(t *testing.T) {
	g = NewGomegaWithT(t)
	testcase := "test-revision-cleanup"
	schema := runtime.NewScheme()
	corev1.AddToScheme(schema)
	appsv1.AddToScheme(schema)
	c = fake.NewClientBuilder().WithScheme(schema).Build()
	g.Expect(createNamespace(c, testcase)).Should(BeNil())

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
	g.Expect(c.Create(context.TODO(), deploy)).Should(BeNil())

	adapter := &OwnerAdapterImpl{
		name:            testcase,
		selector:        selector,
		selectedLabels:  selectedLabels,
		collisionCount:  0,
		historyLimit:    0, // we at lease reserve 2 extra controller revisions
		currentRevision: "test",
		inUsed:          true,
	}

	revisionManager := NewRevisionManager(c, schema, adapter)
	revisionManager.ConstructRevisions(deploy, false)
	waitingCacheUpdate(deploy.Namespace, 1)

	deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v2"
	revisionManager.ConstructRevisions(deploy, false)
	waitingCacheUpdate(deploy.Namespace, 2)

	deploy.Spec.Template.Spec.Containers[0].Image = "nginx:v3"
	revisionManager.ConstructRevisions(deploy, false)
	waitingCacheUpdate(deploy.Namespace, 3)

	revisionManager.ConstructRevisions(deploy, false)
	waitingCacheUpdate(deploy.Namespace, 3)

	adapter.inUsed = false
	revisionManager.ConstructRevisions(deploy, false)
	waitingCacheUpdate(deploy.Namespace, 2)
}

func TestRevisionCreation(t *testing.T) {
	g = NewGomegaWithT(t)
	testcase := "test-revision-creation"
	schema := runtime.NewScheme()
	corev1.AddToScheme(schema)
	appsv1.AddToScheme(schema)
	c = fake.NewClientBuilder().WithScheme(schema).Build()
	g.Expect(createNamespace(c, testcase)).Should(BeNil())

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
	g.Expect(c.Create(context.TODO(), deploy)).Should(BeNil())

	adapter := &OwnerAdapterImpl{
		name:            testcase,
		selector:        selector,
		selectedLabels:  selectedLabels,
		collisionCount:  0,
		historyLimit:    0, // we at lease reserve 2 extra controller revisions
		currentRevision: "test",
		inUsed:          true,
	}

	revisionManager := NewRevisionManager(c, schema, adapter)
	currentRevision, _, _, _, _, err := revisionManager.ConstructRevisions(deploy, false)
	g.Expect(err).Should(BeNil())
	waitingCacheUpdate(deploy.Namespace, 1)

	_, err = revisionManager.createControllerRevision(context.TODO(), deploy, currentRevision, nil)
	g.Expect(err).ShouldNot(BeNil())

	var collisionCount int32 = 0
	// if new revision conflict with existing revision and their contents are equals, then will reuse the existing one
	newRevision, err := revisionManager.newRevision(deploy, currentRevision.Revision+1, &collisionCount)
	g.Expect(err).Should(BeNil())
	newRevision, err = revisionManager.createControllerRevision(context.TODO(), deploy, newRevision, &collisionCount)
	g.Expect(err).Should(BeNil())
	g.Expect(newRevision.Name).Should(BeEquivalentTo(currentRevision.Name))

	// change the data of existing revision
	deployClone := deploy.DeepCopy()
	deployClone.Spec.Template.Labels["foo"] = "foo"
	currentRevision.Data.Raw, _ = revisionManager.ownerGetter.GetPatch(deployClone)
	g.Expect(c.Update(context.TODO(), currentRevision)).Should(BeNil())
	// if their contents are not equals, it should regenerate a new name
	newRevision, err = revisionManager.newRevision(deploy, currentRevision.Revision+1, &collisionCount)
	g.Expect(err).Should(BeNil())
	newRevision, err = revisionManager.createControllerRevision(context.TODO(), deploy, newRevision, &collisionCount)
	g.Expect(err).Should(BeNil())
	g.Expect(newRevision.Name).ShouldNot(BeEquivalentTo(currentRevision.Name))
}

func waitingCacheUpdate(namespace string, expectedRevisionCount int) {
	g.Eventually(func() error {
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

func (a OwnerAdapterImpl) GetCurrentRevision(obj metav1.Object) string {
	return a.currentRevision
}

func (a OwnerAdapterImpl) IsInUsed(obj metav1.Object, controllerRevision string) bool {
	return a.inUsed
}

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
