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

package podopslifecycle

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"kusionstack.io/kube-api/apps/v1alpha1"
)

// PodIDAndTypesMap returns a map of pod id to labels map and a map of operation type to number of pods.
func PodIDAndTypesMap(pod *corev1.Pod) (map[string]map[string]string, map[string]int, error) {
	idToLabelsMap := map[string]map[string]string{}
	typeToNumsMap := map[string]int{}

	ids := sets.String{}
	for k := range pod.Labels {
		if strings.HasPrefix(k, v1alpha1.PodOperatingLabelPrefix) || strings.HasPrefix(k, v1alpha1.PodOperateLabelPrefix) || strings.HasPrefix(k, v1alpha1.PodOperatedLabelPrefix) {
			s := strings.Split(k, "/")
			if len(s) < 2 {
				return nil, nil, fmt.Errorf("invalid label %s", k)
			}
			ids.Insert(s[1])
		}
	}

	for id := range ids {
		for _, val := range []string{v1alpha1.PodOperationTypeLabelPrefix, v1alpha1.PodDoneOperationTypeLabelPrefix} {
			operationType, ok := pod.Labels[fmt.Sprintf("%s/%s", val, id)]
			if !ok {
				continue
			}

			if _, ok := typeToNumsMap[operationType]; !ok {
				typeToNumsMap[operationType] = 1
			} else {
				typeToNumsMap[operationType] = typeToNumsMap[operationType] + 1
			}
			break
		}

		for _, prefix := range v1alpha1.WellKnownLabelPrefixesWithID {
			label := fmt.Sprintf("%s/%s", prefix, id)
			value, ok := pod.Labels[label]
			if !ok {
				continue
			}

			labelsMap, ok := idToLabelsMap[id]
			if !ok {
				labelsMap = make(map[string]string)
				idToLabelsMap[id] = labelsMap
			}
			labelsMap[prefix] = value
		}
	}
	return idToLabelsMap, typeToNumsMap, nil
}

// IsLifecycleOnPod returns true if the lifecycle with lifecycleId exist on pod, otherwise returns false
func IsLifecycleOnPod(lifecycleId string, pod *corev1.Pod) (bool, error) {
	if pod == nil {
		return false, nil
	}
	newIDToLabelsMap, _, err := PodIDAndTypesMap(pod)
	_, exist := newIDToLabelsMap[lifecycleId]
	return exist, err
}

// NumOfLifecycleOnPod returns the nums of lifecycles on pod
func NumOfLifecycleOnPod(pod *corev1.Pod) (int, error) {
	if pod == nil {
		return 0, nil
	}
	newIDToLabelsMap, _, err := PodIDAndTypesMap(pod)
	return len(newIDToLabelsMap), err
}
