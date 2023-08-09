package utils

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

func Test(t *testing.T) {
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
			Replicas: 2,
			Selector: metav1.LabelSelector{
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

	pods := []corev1.Pod{
		{
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

	ref, err := NewRefManager(&MockClient{}, &cs.Spec.Selector, cs, scheme)
	g.Expect(err).Should(gomega.BeNil())

	podObjects := make([]client.Object, len(pods))
	for i := range pods {
		podObjects[i] = &pods[i]
	}

	// adopt orphan pod
	podObjects, err = ref.ClaimOwned(podObjects)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(len(podObjects)).Should(gomega.BeEquivalentTo(1))

	ownerRef := metav1.GetControllerOf(podObjects[0])
	g.Expect(ownerRef).ShouldNot(gomega.BeNil())
	g.Expect(ownerRef.UID).Should(gomega.BeEquivalentTo(cs.UID))

	// release pod not selected
	labels := podObjects[0].GetLabels()
	delete(labels, "app")
	podObjects[0].SetLabels(labels)
	claimedPodObjects, err := ref.ClaimOwned(podObjects)
	g.Expect(err).Should(gomega.BeNil())
	g.Expect(len(claimedPodObjects)).Should(gomega.BeEquivalentTo(0))
	ownerRef = metav1.GetControllerOf(podObjects[0])
	g.Expect(ownerRef).Should(gomega.BeNil())
}

type MockClient struct {
}

func (c *MockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return nil
}

func (c *MockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return nil
}

func (c *MockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return nil
}

func (c *MockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return nil
}

func (c *MockClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	return nil
}
