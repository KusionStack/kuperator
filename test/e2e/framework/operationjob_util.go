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

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type OperationJobTester struct {
	clientSet clientset.Interface
	client    client.Client
	ns        string
}

func NewOperationJobTester(clientSet clientset.Interface, client client.Client, ns string) *OperationJobTester {
	return &OperationJobTester{
		clientSet: clientSet,
		client:    client,
		ns:        ns,
	}
}

func (t *OperationJobTester) NewOperationJob(name string, action appsv1alpha1.OpsAction, targets []appsv1alpha1.PodOpsTarget) *appsv1alpha1.OperationJob {
	return &appsv1alpha1.OperationJob{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: t.ns,
			Name:      name,
		},
		Spec: appsv1alpha1.OperationJobSpec{
			Action:  action,
			Targets: targets,
		},
	}
}

func (t *OperationJobTester) GetOperationJob(oj *appsv1alpha1.OperationJob) error {
	return t.client.Get(context.TODO(), types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
}

func (t *OperationJobTester) CreateOperationJob(oj *appsv1alpha1.OperationJob) error {
	return t.client.Create(context.TODO(), oj)
}

func (t *OperationJobTester) UpdateOperationJob(oj *appsv1alpha1.OperationJob, fn func(oj *appsv1alpha1.OperationJob)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err := t.GetOperationJob(oj)
		if err != nil {
			return err
		}

		fn(oj)
		err = t.client.Update(context.TODO(), oj)
		return err
	})
}

func (t *OperationJobTester) ExpectOperationJobProgress(oj *appsv1alpha1.OperationJob, progress appsv1alpha1.OperationProgress) error {
	err := t.client.Get(context.TODO(), types.NamespacedName{Namespace: oj.Namespace, Name: oj.Name}, oj)
	if err != nil {
		return err
	}
	if oj.Status.Progress != progress {
		return fmt.Errorf("expect progress %s to be is %s", progress, oj.Status.Progress)
	}
	return nil
}

func (t *OperationJobTester) DeleteOperationJob(oj *appsv1alpha1.OperationJob) error {
	return t.client.Delete(context.TODO(), oj)
}

func (t *OperationJobTester) ListPodsForOperationJobTester(oj *appsv1alpha1.OperationJob) (pods []*v1.Pod, err error) {
	podList := &v1.PodList{}
	if err = t.client.List(context.TODO(), podList, client.InNamespace(oj.Namespace)); err != nil {
		return nil, err
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		// ignore deleting pod
		if pod.DeletionTimestamp != nil {
			continue
		}
		if owner := metav1.GetControllerOf(pod); owner != nil && owner.Name == oj.Name {
			pods = append(pods, pod)
		}
	}
	sort.SliceStable(pods, func(i, j int) bool {
		return pods[i].Name < pods[j].Name
	})
	return
}

func (t *OperationJobTester) UpdatePod(pod *v1.Pod, fn func(pod *v1.Pod)) error {
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

func (t *OperationJobTester) GetForOperationJobTester(oj *appsv1alpha1.OperationJob, pod *v1.Pod) (CRR *kruisev1alpha1.ContainerRecreateRequest, err error) {
	CRR = &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", oj.Name, pod.Name)
	err = t.client.Get(context.TODO(), types.NamespacedName{Namespace: oj.Namespace, Name: crrName}, CRR)
	return CRR, err
}
