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
	"fmt"
	"sort"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
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

func (t *CollaSetTester) CreateCollaSet(cls *appsv1alpha1.CollaSet) error {
	return t.client.Create(context.TODO(), cls)
}

func (t *CollaSetTester) GetCollaSet(cls *appsv1alpha1.CollaSet) error {
	return t.client.Get(context.TODO(), types.NamespacedName{Namespace: cls.Namespace, Name: cls.Name}, cls)
}

func (t *CollaSetTester) UpdateCollaSet(cls *appsv1alpha1.CollaSet, fn func(cls *appsv1alpha1.CollaSet)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err := t.GetCollaSet(cls)
		if err != nil {
			return err
		}

		fn(cls)
		err = t.client.Update(context.TODO(), cls)
		return err
	})
}

func (t *CollaSetTester) ListPodsForCollaSet(cls *appsv1alpha1.CollaSet) (pods []*v1.Pod, err error) {
	podList := &v1.PodList{}
	if err = t.client.List(context.TODO(), podList); err != nil {
		return nil, err
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		// ignore deleting pod
		if pod.DeletionTimestamp != nil {
			continue
		}
		if owner := metav1.GetControllerOf(pod); owner != nil && owner.Name == cls.Name {
			pods = append(pods, pod)
		}
	}
	sort.SliceStable(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
	return
}

func (t *CollaSetTester) ListPVCForCloneSet(cls *appsv1alpha1.CollaSet) (pvcs []*v1.PersistentVolumeClaim, err error) {
	pvcList := &v1.PersistentVolumeClaimList{}
	err = t.client.List(context.TODO(), pvcList, client.InNamespace(cls.Namespace))
	if err != nil {
		return nil, err
	}
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if pvc.DeletionTimestamp.IsZero() {
			pvcs = append(pvcs, pvc)
		}
	}
	sort.SliceStable(pvcs, func(i, j int) bool {
		return pvcs[i].Name < pvcs[j].Name
	})
	return
}

func (t *CollaSetTester) DeleteCollaSet(cls *appsv1alpha1.CollaSet) error {
	return t.client.Delete(context.TODO(), cls)
}

func (t *CollaSetTester) UpdatePod(pod *v1.Pod, fn func(pod *v1.Pod)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err := t.client.Get(context.TODO(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, pod)
		if err != nil {
			return err
		}

		fn(pod)
		err = t.client.Update(context.TODO(), pod)
		return err
	})
}

func (t *CollaSetTester) ExpectedStatusReplicas(cls *appsv1alpha1.CollaSet, replicas, readyReplicas, availableReplicas, updatedReplicas, totalReplicas int32) error {
	if err := t.client.Get(context.TODO(), types.NamespacedName{Namespace: cls.Namespace, Name: cls.Name}, cls); err != nil {
		return err
	}

	if cls.Status.Replicas != replicas {
		return fmt.Errorf("replicas got %d, expected %d", cls.Status.Replicas, replicas)
	}

	if cls.Status.ReadyReplicas != readyReplicas {
		return fmt.Errorf("readyReplicas got %d, expected %d", cls.Status.ReadyReplicas, readyReplicas)
	}

	if cls.Status.AvailableReplicas != availableReplicas {
		return fmt.Errorf("availableReplicas got %d, expected %d", cls.Status.AvailableReplicas, availableReplicas)
	}

	if cls.Status.UpdatedReplicas != updatedReplicas {
		return fmt.Errorf("updatedReplicas got %d, expected %d", cls.Status.UpdatedReplicas, updatedReplicas)
	}

	if pods, err := t.ListPodsForCollaSet(cls); err != nil {
		return err
	} else if len(pods) != int(totalReplicas) {
		return fmt.Errorf("totalReplicas got %d, expected %d", len(pods), totalReplicas)
	}

	return nil
}
