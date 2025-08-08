/*
Copyright 2016 The Kubernetes Authors.
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

package utils

import (
	"context"
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestAdopt(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	scheme := runtime.NewScheme()
	appsv1alpha1.AddToScheme(scheme)

	testName := "ref-manager"
	testcases := []struct {
		name                 string
		isSelectorMatch      bool
		isPodExist           bool
		isOwnerInTerminating bool
		isPodInTerminating   bool
		isConflict           bool
		expectErr            bool
		expectAdopted        bool
	}{
		{
			name:                 "happy pass",
			isSelectorMatch:      true,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectAdopted:        true,
		},
		{
			name:                 "update conflict",
			isSelectorMatch:      true,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           true,
			expectErr:            true,
			expectAdopted:        false,
		},
		{
			name:                 "not match",
			isSelectorMatch:      false,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectAdopted:        false,
		},
		{
			name:                 "pod not exist",
			isSelectorMatch:      true,
			isPodExist:           false,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectAdopted:        false,
		},
		{
			name:                 "owner in terminating",
			isSelectorMatch:      true,
			isPodExist:           true,
			isOwnerInTerminating: true,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectAdopted:        false,
		},
		{
			name:                 "pod in terminating",
			isSelectorMatch:      true,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   true,
			isConflict:           false,
			expectErr:            false,
			expectAdopted:        false,
		},
	}

	for _, testcase := range testcases {
		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				UID:       types.UID("fake-uid"),
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"case": testName,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"case": testName,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}

		objs := []client.Object{
			&appsv1alpha1.ResourceContext{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					Labels: map[string]string{
						"app":  "foo",
						"case": testName,
					},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ref, err := NewRefManager(c, cs.Spec.Selector, cs, scheme)
		g.Expect(err).Should(gomega.BeNil())

		if !testcase.isSelectorMatch {
			for _, obj := range objs {
				obj.GetLabels()["app"] = "foo1"
			}
		}

		if testcase.isPodExist {
			for _, obj := range objs {
				g.Expect(c.Create(context.TODO(), obj)).Should(gomega.BeNil())
			}
		}

		now := metav1.Now()
		if testcase.isOwnerInTerminating {
			cs.DeletionTimestamp = &now
		}

		if testcase.isPodInTerminating {
			for _, obj := range objs {
				obj.SetDeletionTimestamp(&now)
			}
		}

		if testcase.isConflict {
			for _, obj := range objs {
				obj.SetResourceVersion("100")
			}
		}

		podObjects, err := ref.ClaimOwned(objs)
		if testcase.expectErr {
			g.Expect(err).ShouldNot(gomega.BeNil(), fmt.Sprintf("case: %s", testcase.name))
		} else {
			g.Expect(err).Should(gomega.BeNil(), fmt.Sprintf("case: %s", testcase.name))
		}

		if testcase.expectAdopted {
			g.Expect(len(podObjects)).Should(gomega.BeEquivalentTo(1), fmt.Sprintf("case: %s", testcase.name))
		} else {
			g.Expect(len(podObjects)).Should(gomega.BeEquivalentTo(0), fmt.Sprintf("case: %s", testcase.name))
		}
	}
}

func TestRelease(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	scheme := runtime.NewScheme()
	appsv1alpha1.AddToScheme(scheme)

	testName := "ref-manager"
	testcases := []struct {
		name                 string
		isOwnedByIt          bool
		isSelectorMatch      bool
		isPodExist           bool
		isOwnerInTerminating bool
		isPodInTerminating   bool
		isConflict           bool
		expectErr            bool
		expectReleased       bool
	}{
		{
			name:                 "happy pass",
			isOwnedByIt:          true,
			isSelectorMatch:      true,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectReleased:       false,
		},
		{
			name:                 "update conflict",
			isOwnedByIt:          true,
			isSelectorMatch:      false,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           true,
			expectErr:            true,
			expectReleased:       false,
		},
		{
			name:                 "not own pod",
			isOwnedByIt:          false,
			isSelectorMatch:      true,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectReleased:       true,
		},
		{
			name:                 "not match",
			isOwnedByIt:          true,
			isSelectorMatch:      false,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectReleased:       true,
		},
		{
			name:                 "pod not exist",
			isOwnedByIt:          true,
			isSelectorMatch:      false,
			isPodExist:           false,
			isOwnerInTerminating: false,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectReleased:       true,
		},
		{
			name:                 "owner in terminating",
			isOwnedByIt:          true,
			isSelectorMatch:      false,
			isPodExist:           true,
			isOwnerInTerminating: true,
			isPodInTerminating:   false,
			isConflict:           false,
			expectErr:            false,
			expectReleased:       false,
		},
		{
			name:                 "pod in terminating",
			isOwnedByIt:          true,
			isSelectorMatch:      false,
			isPodExist:           true,
			isOwnerInTerminating: false,
			isPodInTerminating:   true,
			isConflict:           false,
			expectErr:            false,
			expectReleased:       true,
		},
	}

	for _, testcase := range testcases {
		cs := &appsv1alpha1.CollaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "foo",
				Namespace: "default",
				UID:       types.UID("fake-uid"),
			},
			Spec: appsv1alpha1.CollaSetSpec{
				Replicas: int32Pointer(2),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app":  "foo",
						"case": testName,
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app":  "foo",
							"case": testName,
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "nginx",
								Image: "nginx:v1",
							},
						},
					},
				},
			},
		}

		objs := []client.Object{
			&appsv1alpha1.ResourceContext{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					Labels: map[string]string{
						"app":  "foo",
						"case": testName,
					},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		ref, err := NewRefManager(c, cs.Spec.Selector, cs, scheme)
		g.Expect(err).Should(gomega.BeNil())
		for _, obj := range objs {
			if !testcase.isOwnedByIt {
				boolean := true
				obj.SetOwnerReferences([]metav1.OwnerReference{
					{
						UID:        "other-uid",
						Controller: &boolean,
					},
				})
			}
			g.Expect(c.Create(context.TODO(), obj)).Should(gomega.BeNil())
		}

		if testcase.isOwnedByIt {
			podObjects, err := ref.ClaimOwned(objs)
			g.Expect(err).Should(gomega.BeNil())
			g.Expect(len(podObjects)).Should(gomega.BeEquivalentTo(len(objs)))
		}

		if !testcase.isSelectorMatch {
			for _, obj := range objs {
				obj.GetLabels()["app"] = "foo1"
			}
		}

		if !testcase.isPodExist {
			for _, obj := range objs {
				g.Expect(c.Delete(context.TODO(), obj)).Should(gomega.BeNil())
			}
		}

		now := metav1.Now()
		if testcase.isOwnerInTerminating {
			cs.DeletionTimestamp = &now
		}

		if testcase.isPodInTerminating {
			for _, obj := range objs {
				obj.SetDeletionTimestamp(&now)
			}
		}

		if testcase.isConflict {
			for _, obj := range objs {
				obj.SetResourceVersion("100")
			}
		}

		_, err = ref.ClaimOwned(objs)
		rcList := &appsv1alpha1.ResourceContextList{}
		g.Expect(c.List(context.TODO(), rcList, client.InNamespace(cs.Namespace))).Should(gomega.BeNil())
		if testcase.expectErr {
			g.Expect(err).ShouldNot(gomega.BeNil(), fmt.Sprintf("case: %s", testcase.name))
		} else {
			g.Expect(err).Should(gomega.BeNil(), fmt.Sprintf("case: %s", testcase.name))
		}

		if testcase.expectReleased {
			g.Expect(len(filterOwnedObj(rcList.Items, cs))).Should(gomega.BeEquivalentTo(0), fmt.Sprintf("case: %s", testcase.name))
		} else {
			g.Expect(len(filterOwnedObj(rcList.Items, cs))).Should(gomega.BeEquivalentTo(1), fmt.Sprintf("case: %s", testcase.name))
		}
	}
}

func filterOwnedObj(objs []appsv1alpha1.ResourceContext, owner client.Object) []client.Object {
	var res []client.Object
	for _, obj := range objs {
		ownerRef := metav1.GetControllerOf(&obj)
		if ownerRef == nil {
			continue
		}

		if ownerRef.UID != owner.GetUID() {
			continue
		}

		res = append(res, &obj)
	}

	return res
}

func TestCoverCornerCase(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	scheme := runtime.NewScheme()
	appsv1alpha1.AddToScheme(scheme)

	testcase := "ref-manager"
	cs := &appsv1alpha1.CollaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
			UID:       types.UID("fake-uid"),
		},
		Spec: appsv1alpha1.CollaSetSpec{
			Replicas: int32Pointer(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":  "foo",
					"case": testcase,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app":  "foo",
						"case": testcase,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:v1",
						},
					},
				},
			},
		},
	}

	objs := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels: map[string]string{
					"app":  "foo",
					"case": testcase,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "nginx",
						Image: "nginx:v1",
					},
				},
			},
		},
	}

	c := fake.NewClientBuilder().Build()
	ref, err := NewRefManager(c, cs.Spec.Selector, cs, nil)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(ref.adopt(objs[0])).ShouldNot(gomega.BeNil())

	ref, err = NewRefManager(c, cs.Spec.Selector, cs, runtime.NewScheme())
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(ref.adopt(objs[0])).ShouldNot(gomega.BeNil())

	ref, err = NewRefManager(c, &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"_1": "invalid",
		},
	}, cs, scheme)
	g.Expect(err).ShouldNot(gomega.BeNil())

	ref, err = NewRefManager(c, cs.Spec.Selector, cs, scheme)
	g.Expect(err).Should(gomega.BeNil())

	now := metav1.Now()
	cs.DeletionTimestamp = &now
	g.Expect(ref.adopt(objs[0])).ShouldNot(gomega.BeNil())
}

func int32Pointer(val int32) *int32 {
	return &val
}
