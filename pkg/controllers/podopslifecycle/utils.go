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

	v1 "k8s.io/api/core/v1"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

// PodIDAndTypesMap returns a map of pod id to labels map and a map of operation type to number of pods.
func PodIDAndTypesMap(pod *v1.Pod) (map[string]map[string]string, map[string]int, error) {
	idToLabelsMap := map[string]map[string]string{}
	typeToNumsMap := map[string]int{}

	for k, v := range pod.Labels {
		if strings.HasPrefix(k, v1alpha1.PodOperationTypeLabelPrefix) {
			if _, ok := typeToNumsMap[v]; !ok {
				typeToNumsMap[v] = 1
			} else {
				typeToNumsMap[v] = typeToNumsMap[v] + 1
			}
		}

		for _, label := range v1alpha1.WellKnownLabelPrefixesWithID {
			if !strings.HasPrefix(k, label) {
				continue
			}

			s := strings.Split(k, "/")
			if len(s) < 2 {
				return nil, nil, fmt.Errorf("invalid label %s", k)
			}
			id := s[1]

			labelsMap, ok := idToLabelsMap[id]
			if !ok {
				labelsMap = make(map[string]string)
				idToLabelsMap[id] = labelsMap
			}
			labelsMap[label] = v

			break
		}
	}
	return idToLabelsMap, typeToNumsMap, nil
}
