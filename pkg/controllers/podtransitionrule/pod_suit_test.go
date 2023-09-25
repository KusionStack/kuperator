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

package podtransitionrule

import (
	"crypto/rand"
	"net"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func randomIP() net.IP {
	ip := make(net.IP, 4)
	rand.Read(ip)
	return ip
}

func schedule(po *corev1.Pod) *corev1.Pod {
	updateCondition(po, &corev1.PodCondition{
		Type:   "PodScheduled",
		Status: corev1.ConditionTrue,
	})
	updateCondition(po, &corev1.PodCondition{
		Type:   "IPAllocated",
		Status: corev1.ConditionTrue,
	})
	updateCondition(po, &corev1.PodCondition{
		Type:   "NamingRegistered",
		Status: corev1.ConditionTrue,
	})
	po.Status.PodIP = randomIP().String()
	return po
}

func setContainerNotReady(po *corev1.Pod) *corev1.Pod {
	var containerNames []string
	for _, cn := range po.Spec.Containers {
		containerNames = append(containerNames, cn.Name)
	}
	updateCondition(po, &corev1.PodCondition{
		Type:   corev1.ContainersReady,
		Status: corev1.ConditionFalse,
	})
	updateCondition(po, &corev1.PodCondition{
		Type:   "Ready",
		Status: corev1.ConditionFalse,
	})
	tr := false
	po.Status.ContainerStatuses = []corev1.ContainerStatus{}
	po.Status.Phase = corev1.PodRunning
	for _, name := range containerNames {
		po.Status.ContainerStatuses = append(po.Status.ContainerStatuses, corev1.ContainerStatus{
			Name:    name,
			Ready:   false,
			Started: &tr,
		})
	}
	return po
}

func setContainersReady(po *corev1.Pod) *corev1.Pod {
	var containerNames []string
	for _, cn := range po.Spec.Containers {
		containerNames = append(containerNames, cn.Name)
	}
	updateCondition(po, &corev1.PodCondition{
		Type:   corev1.ContainersReady,
		Status: corev1.ConditionTrue,
	})
	updateCondition(po, &corev1.PodCondition{
		Type:   "NamingRegistered",
		Status: corev1.ConditionTrue,
	})
	updateCondition(po, &corev1.PodCondition{
		Type:   "Ready",
		Status: corev1.ConditionTrue,
	})
	tr := true
	timeNow := metav1.Now()
	po.Status.Phase = corev1.PodRunning
	for _, name := range containerNames {
		po.Status.ContainerStatuses = append(po.Status.ContainerStatuses, corev1.ContainerStatus{
			Name:    name,
			Ready:   true,
			Started: &tr,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: timeNow,
				},
			},
		})
	}
	return po
}

func updateCondition(po *corev1.Pod, condition *corev1.PodCondition) {
	find := false
	for i, cd := range po.Status.Conditions {
		if cd.Type == condition.Type {
			po.Status.Conditions[i] = *condition
			find = true
			break
		}
	}
	if !find {
		po.Status.Conditions = append(po.Status.Conditions, *condition)
	}
}
