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

package utils

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/utils"
)

type PodWrapper struct {
	*corev1.Pod
	ID            int
	ContextDetail *appsv1alpha1.ContextDetail
}

func CollectPodInstanceID(pods []*PodWrapper) map[int]struct{} {
	podInstanceIDSet := map[int]struct{}{}
	for _, pod := range pods {
		podInstanceIDSet[pod.ID] = struct{}{}
	}

	return podInstanceIDSet
}

func GetPodInstanceID(pod *corev1.Pod) (int, error) {
	if pod.Labels == nil {
		return -1, fmt.Errorf("no labels found for instance ID")
	}

	val, exist := pod.Labels[appsv1alpha1.PodInstanceIDLabelKey]
	if !exist {
		return -1, fmt.Errorf("failed to find instance ID label %s", appsv1alpha1.PodInstanceIDLabelKey)
	}

	id, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		// ignore invalid pod instance ID
		return -1, fmt.Errorf("failed to parse instance ID with value %s: %s", val, err)
	}

	return int(id), nil
}

func NewPodFrom(owner metav1.Object, ownerRef *metav1.OwnerReference, revision *appsv1.ControllerRevision) (*corev1.Pod, error) {
	pod, err := controllerutils.NewPodFrom(owner, ownerRef, revision)
	if err != nil {
		return nil, err
	}

	utils.ControllByKusionStack(pod)
	return pod, nil
}
