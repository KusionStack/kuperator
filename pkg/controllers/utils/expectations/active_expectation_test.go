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

package expectations

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "foo", Namespace: "default"}}

var depKey = types.NamespacedName{Name: "foo-deployment", Namespace: "default"}

func Setup(t *testing.T) (g *gomega.GomegaWithT, r reconcile.Reconciler, mgr manager.Manager, client client.Client, stopFun func()) {
	g = gomega.NewGomegaWithT(t)
	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
	})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	stopMgr, mgrStopped := StartTestManager(mgr, g)
	return g, r, mgr, mgr.GetClient(), func() {
		close(stopMgr)
		mgrStopped.Wait()
	}
}

func TestReconcileResponsePodEvent(t *testing.T) {
	g, _, _, client, stopFunc := Setup(t)
	defer func() {
		stopFunc()
	}()

	exp := NewActiveExpectations(client)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "parent",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "image:v1",
				},
			},
		},
	}
	g.Expect(client.Create(context.TODO(), pod)).Should(gomega.BeNil())

	podA := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    "default",
			GenerateName: "test-",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "image:v1",
				},
			},
		},
	}
	g.Expect(client.Create(context.TODO(), podA)).Should(gomega.BeNil())
	time.Sleep(3 * time.Second)

	podBName := "test-b"
	g.Expect(exp.ExpectCreate(pod, Pod, podA.Name)).Should(gomega.BeNil())
	g.Expect(exp.ExpectCreate(pod, Pod, podBName)).Should(gomega.BeNil())
	expectation, err := exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(len(expectation.items.List())).Should(gomega.BeEquivalentTo(2))

	sa, err := exp.IsSatisfied(pod)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(sa).Should(gomega.BeFalse())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(len(expectation.items.List())).Should(gomega.BeEquivalentTo(1))

	podB := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      podBName,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "image:v1",
				},
			},
		},
	}
	g.Expect(client.Create(context.TODO(), podB)).Should(gomega.BeNil())
	time.Sleep(3 * time.Second)

	sa, err = exp.IsSatisfied(pod)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(sa).Should(gomega.BeTrue())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())

	// expected update
	podB.Labels = map[string]string{
		"test": "test",
	}
	g.Expect(client.Update(context.TODO(), podB)).Should(gomega.BeNil())
	exp.ExpectUpdate(pod, Pod, pod.Name, pod.ResourceVersion)
	time.Sleep(3 * time.Second)

	sa, err = exp.IsSatisfied(pod)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(sa).Should(gomega.BeTrue())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())

	g.Expect(exp.ExpectDelete(pod, Pod, podBName)).Should(gomega.BeNil())
	g.Expect(exp.ExpectDelete(pod, Pod, podBName)).Should(gomega.BeNil())
	sa, err = exp.IsSatisfied(pod)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(sa).Should(gomega.BeFalse())

	g.Expect(client.Delete(context.TODO(), podA)).Should(gomega.BeNil())
	time.Sleep(3 * time.Second)

	sa, err = exp.IsSatisfied(pod)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(sa).Should(gomega.BeFalse())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(len(expectation.items.List())).Should(gomega.BeEquivalentTo(1))

	g.Expect(client.Delete(context.TODO(), podB)).Should(gomega.BeNil())
	time.Sleep(3 * time.Second)

	sa, err = exp.IsSatisfied(pod)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(sa).Should(gomega.BeTrue())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())

	g.Expect(exp.ExpectCreate(pod, Pod, podA.Name)).Should(gomega.BeNil())
	g.Expect(exp.Delete(pod.Namespace, pod.Name)).Should(gomega.BeNil())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())
}
