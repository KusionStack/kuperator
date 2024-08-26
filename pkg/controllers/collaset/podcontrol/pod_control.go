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

package podcontrol

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	refmanagerutil "kusionstack.io/kuperator/pkg/controllers/utils/refmanager"
	"kusionstack.io/kuperator/pkg/utils/inject"
)

type Interface interface {
	GetFilteredPods(selector *metav1.LabelSelector, owner client.Object) ([]*corev1.Pod, error)
	CreatePod(pod *corev1.Pod) (*corev1.Pod, error)
	DeletePod(pod *corev1.Pod) error
	UpdatePod(pod *corev1.Pod) error
	PatchPod(pod *corev1.Pod, patch client.Patch) error
}

func NewRealPodControl(client client.Client, scheme *runtime.Scheme) Interface {
	return &RealPodControl{
		client: client,
		scheme: scheme,
	}
}

type RealPodControl struct {
	client client.Client
	scheme *runtime.Scheme
}

func (pc *RealPodControl) GetFilteredPods(selector *metav1.LabelSelector, owner client.Object) ([]*corev1.Pod, error) {
	// get the pods with ownerReference points to this CollaSet
	podList := &corev1.PodList{}
	err := pc.client.List(context.TODO(), podList, &client.ListOptions{Namespace: owner.GetNamespace(),
		FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexOwnerRefUID, string(owner.GetUID()))})
	if err != nil {
		return nil, err
	}

	filteredPods := FilterOutInactivePod(podList.Items)
	filteredPods, err = pc.getPodSetPods(filteredPods, selector, owner)
	if err != nil {
		return nil, err
	}

	return filteredPods, nil
}

func (pc *RealPodControl) CreatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	if err := pc.client.Create(context.TODO(), pod); err != nil {
		return nil, fmt.Errorf("fail to create Pod: %s", err)
	}

	return pod, nil
}

func (pc *RealPodControl) DeletePod(pod *corev1.Pod) error {
	return pc.client.Delete(context.TODO(), pod)
}

func (pc *RealPodControl) UpdatePod(pod *corev1.Pod) error {
	return pc.client.Update(context.TODO(), pod)
}

func (pc *RealPodControl) PatchPod(pod *corev1.Pod, patch client.Patch) error {
	return pc.client.Patch(context.TODO(), pod, patch)
}

func (pc *RealPodControl) getPodSetPods(pods []*corev1.Pod, selector *metav1.LabelSelector, owner client.Object) ([]*corev1.Pod, error) {
	// Use ControllerRefManager to adopt/orphan as needed.
	cm, err := refmanagerutil.NewRefManager(pc.client, selector, owner, pc.scheme)
	if err != nil {
		return nil, fmt.Errorf("fail to create ref manager: %s", err)
	}

	var candidates = make([]client.Object, len(pods))
	for i, pod := range pods {
		candidates[i] = pod
	}

	claims, err := cm.ClaimOwned(candidates)
	if err != nil {
		return nil, err
	}

	claimPods := make([]*corev1.Pod, len(claims))
	for i, mt := range claims {
		claimPods[i] = mt.(*corev1.Pod)
	}

	return claimPods, nil
}

func FilterOutInactivePod(pods []corev1.Pod) []*corev1.Pod {
	var filteredPod []*corev1.Pod

	for i := range pods {
		if IsPodInactive(&pods[i]) {
			continue
		}

		filteredPod = append(filteredPod, &pods[i])
	}
	return filteredPod
}

func IsPodInactive(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}
