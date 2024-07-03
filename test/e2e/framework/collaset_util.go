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

package framework

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	imageutils "k8s.io/kubernetes/test/utils/image"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type CollaSetTester struct {
	clientSet clientset.Interface
	client    client.Client
	ns        string
}

func NewCollaSetTester(clientSet clientset.Interface, client client.Client, ns string) *CollaSetTester {
	return &CollaSetTester{
		clientSet: clientSet,
		client:    client,
		ns:        ns,
	}
}

func (t *CollaSetTester) NewCollaSet(name string, replicas int32, updateStrategy appsv1alpha1.UpdateStrategy) *appsv1alpha1.CollaSet {
	return &appsv1alpha1.CollaSet{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: t.ns,
			Name:      name,
		},
		Spec: appsv1alpha1.CollaSetSpec{
			Replicas:       &replicas,
			Selector:       &metav1.LabelSelector{MatchLabels: map[string]string{"owner": name}},
			UpdateStrategy: updateStrategy,
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"owner": name},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "nginx",
							Image: imageutils.GetE2EImage(imageutils.Nginx),
							Env: []v1.EnvVar{
								{Name: "test", Value: "foo"},
							},
						},
					},
				},
			},
		},
	}
}

func (t *CollaSetTester) CreateCollaSet(cs *appsv1alpha1.CollaSet) error {
	return t.client.Create(context.TODO(), cs)
}

func (t *CollaSetTester) GetCollaSet(cs *appsv1alpha1.CollaSet) error {
	return t.client.Get(context.TODO(), types.NamespacedName{Namespace: cs.Namespace, Name: cs.Name}, cs)
}
