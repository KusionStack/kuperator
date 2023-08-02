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
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

const (
	MaxConcurrencyReconcileTimes float64 = 1
)

func KeyFunc(obj metav1.Object) string {
	return Key(obj.GetNamespace(), obj.GetName())
}

func Key(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func IDAndTypesMap(pod *v1.Pod) (map[string]map[string]string, map[string]int, error) {
	idToLabelsMap := map[string]map[string]string{}
	typeToNumsMap := map[string]int{}

	wellKnownlabels := []string{v1alpha1.LabelPodOperating, v1alpha1.LabelPodOperationType, v1alpha1.LabelPodPreCheck, v1alpha1.LabelPodPreChecked,
		v1alpha1.LabelPodPrepare, v1alpha1.LabelPodUndoOperationType, v1alpha1.LabelPodOperate, v1alpha1.LabelPodOperated, v1alpha1.LabelPodPostCheck,
		v1alpha1.LabelPodPostChecked, v1alpha1.LabelPodComplete}

	for k, v := range pod.Labels {
		if strings.HasPrefix(k, v1alpha1.LabelPodOperationType) {
			if _, ok := typeToNumsMap[v]; !ok {
				typeToNumsMap[v] = 1
			} else {
				typeToNumsMap[v] = typeToNumsMap[v] + 1
			}
		}

		for _, label := range wellKnownlabels {
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
