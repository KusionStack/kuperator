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
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestActiveExpectations(t *testing.T) {
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
	g.Eventually(func() error {
		sa, err = exp.IsSatisfied(pod)
		if err != nil {
			return err
		}

		if !sa {
			return fmt.Errorf("expectation unsatisfied")
		}

		return nil
	}, 5*time.Second, 1*time.Second).Should(gomega.BeNil())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())

	// expected update
	podB.Labels = map[string]string{
		"test": "test",
	}
	g.Expect(client.Update(context.TODO(), podB)).Should(gomega.BeNil())
	g.Expect(exp.ExpectUpdate(pod, Pod, pod.Name, pod.ResourceVersion)).Should(gomega.BeNil())
	// update twice
	podB.Labels = map[string]string{
		"test": "foo",
	}
	g.Expect(client.Update(context.TODO(), podB)).Should(gomega.BeNil())
	g.Expect(exp.ExpectUpdate(pod, Pod, pod.Name, pod.ResourceVersion)).Should(gomega.BeNil())
	g.Eventually(func() error {
		sa, err = exp.IsSatisfied(pod)
		if err != nil {
			return err
		}

		if !sa {
			return fmt.Errorf("expectation unsatisfied")
		}

		return nil
	}, 5*time.Second, 1*time.Second).Should(gomega.BeNil())
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
	g.Eventually(func() error {
		sa, err = exp.IsSatisfied(pod)
		if err != nil {
			return err
		}

		if !sa {
			return fmt.Errorf("expectation unsatisfied")
		}

		return nil
	}, 5*time.Second, 1*time.Second).Should(gomega.BeNil())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())

	g.Expect(exp.ExpectCreate(pod, Pod, podA.Name)).Should(gomega.BeNil())
	g.Expect(exp.Delete(pod.Namespace, pod.Name)).Should(gomega.BeNil())
	expectation, err = exp.GetExpectation(pod.Namespace, pod.Name)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(expectation).Should(gomega.BeNil())

	g.Expect(exp.ExpectCreate(pod, Pod, podA.Name)).Should(gomega.BeNil())
	g.Expect(exp.IsSatisfied(pod)).Should(gomega.BeFalse())
	g.Expect(exp.DeleteItem(pod, Pod, podA.Name)).Should(gomega.BeNil())
	g.Expect(exp.IsSatisfied(pod)).Should(gomega.BeTrue())
	// delete no existing item should not get error
	g.Expect(exp.DeleteItem(pod, Pod, podA.Name)).Should(gomega.BeNil())
}

func TestActiveExpectationsForAllKinds(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	for kind, newFn := range ResourceInitializers {
		g.Expect(newFn()).ShouldNot(gomega.BeNil(), fmt.Sprintf("check initializer for kind %s", kind))
	}
}

func TestActiveExpectationsValidationForPanics(t *testing.T) {
	_, _, _, client, stopFunc := Setup(t)
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
	assertPanic(t, func() {
		exp.expect(pod, "no-existing", pod.Name, Create)
	})

	assertPanic(t, func() {
		exp.expect(pod, Pod, pod.Name, Update)
	})

	assertPanic(t, func() {
		exp.ExpectUpdate(pod, Pod, pod.Name, "string-type")
	})

	assertPanic(t, func() {
		exp.ExpectUpdate(pod, "no-existing", pod.Name, "1")
	})
}

func assertPanic(t *testing.T, fn func()) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, got nil")
		}
	}()

	fn()
}
