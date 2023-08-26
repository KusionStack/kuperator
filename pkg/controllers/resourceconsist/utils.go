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

package resourceconsist

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

func isPod(obj client.Object) bool {
	_, ok := obj.(*corev1.Pod)
	return ok
}

// isPodLifecycleReady check both ReadinessGatePodServiceReady and PodReady to avoid effects that PodReady not set to false
// even ReadinessGatePodServiceReady is false caused by evict machine
func isPodLifecycleReady(pod *corev1.Pod) bool {
	if !pod.DeletionTimestamp.IsZero() {
		return false
	}

	podReady := false
	readinessGateReady := true
	var podReadyChecked, readinessGateReadyChecked bool
	for _, condition := range pod.Status.Conditions {
		if podReadyChecked && readinessGateReadyChecked {
			break
		}
		if condition.Type == corev1.PodReady {
			podReadyChecked = true
			if condition.Status == corev1.ConditionTrue {
				podReady = true
				continue
			}
			break
		}
		if condition.Type == v1alpha1.ReadinessGatePodServiceReady {
			readinessGateReadyChecked = true
			if condition.Status != corev1.ConditionTrue {
				readinessGateReady = false
				break
			}
		}
	}
	return podReady && readinessGateReady
}

func getPodIpv6Address(pod *corev1.Pod) string {
	if pod == nil {
		return ""
	}
	for _, ip := range pod.Status.PodIPs {
		if net.ParseIP(ip.IP) != nil && strings.Contains(ip.IP, ":") {
			return ip.IP
		}
	}
	return ""
}

// GetCommonPodEmployeeStatus called by ReconcileAdapter's GetExpectEmployee/GetCurrentEmployee
func GetCommonPodEmployeeStatus(pod *corev1.Pod) (PodEmployeeStatuses, error) {
	if pod == nil {
		return PodEmployeeStatuses{}, errors.New("SetCommonPodEmployeeStatus failed, pod is nil")
	}

	return PodEmployeeStatuses{
		Ip:             pod.Status.PodIP,
		Ipv6:           getPodIpv6Address(pod),
		LifecycleReady: isPodLifecycleReady(pod),
	}, nil
}

func GenerateLifecycleFinalizerKey(employer client.Object) string {
	return fmt.Sprintf("%s/%s/%s", employer.GetObjectKind().GroupVersionKind().Kind,
		employer.GetNamespace(), employer.GetName())
}

func GenerateLifecycleFinalizer(employerName string) string {
	b := md5.Sum([]byte(employerName))
	return v1alpha1.PodOperationProtectionFinalizerPrefix + "/" + hex.EncodeToString(b[:])[8:24]
}
