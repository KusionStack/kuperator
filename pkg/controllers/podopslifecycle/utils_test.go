/**
 * Copyright 2023 KusionStack Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package podopslifecycle

import (
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kusionstack.io/operating/apis/apps/v1alpha1"
)

func TestPodIDAndTypesMap(t *testing.T) {
	var (
		name      = "test"
		namespace = "default"
	)
	casee := []struct {
		keyWords string

		labels map[string]string

		idToLabelsMap map[string]map[string]string
		typeToNumsMap map[string]int
		err           error
	}{
		{
			keyWords: "A pod with wrong label",
			labels: map[string]string{
				v1alpha1.PodOperatingLabelPrefix: "1402144848",
			},

			idToLabelsMap: nil,
			typeToNumsMap: nil,
			err:           fmt.Errorf("invalid label %s", v1alpha1.PodOperatingLabelPrefix),
		},

		{
			keyWords: "A pod with no label",
			labels:   nil,

			idToLabelsMap: nil,
			typeToNumsMap: nil,
			err:           nil,
		},

		{
			keyWords: "A pod with normal labels",
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "abc",
			},

			idToLabelsMap: map[string]map[string]string{
				"123": {
					v1alpha1.PodOperatingLabelPrefix:     "1402144848",
					v1alpha1.PodOperationTypeLabelPrefix: "abc",
				},
			},
			typeToNumsMap: map[string]int{
				"abc": 1,
			},
			err: nil,
		},

		{
			keyWords: "A pod with multi labels which have different types",
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "abc",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144849",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "def",
			},

			idToLabelsMap: map[string]map[string]string{
				"123": {
					v1alpha1.PodOperatingLabelPrefix:     "1402144848",
					v1alpha1.PodOperationTypeLabelPrefix: "abc",
				},
				"456": {
					v1alpha1.PodOperatingLabelPrefix:     "1402144849",
					v1alpha1.PodOperationTypeLabelPrefix: "def",
				},
			},
			typeToNumsMap: map[string]int{
				"abc": 1,
				"def": 1,
			},
			err: nil,
		},

		{
			keyWords: "A pod with multi labels which have the same type",
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "abc",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144849",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "abc",
			},

			idToLabelsMap: map[string]map[string]string{
				"123": {
					v1alpha1.PodOperatingLabelPrefix:     "1402144848",
					v1alpha1.PodOperationTypeLabelPrefix: "abc",
				},
				"456": {
					v1alpha1.PodOperatingLabelPrefix:     "1402144849",
					v1alpha1.PodOperationTypeLabelPrefix: "abc",
				},
			},
			typeToNumsMap: map[string]int{
				"abc": 2,
			},
			err: nil,
		},
	}

	for _, c := range casee {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    c.labels,
			},
		}

		idToLabelsMap, typeToNumsMap, err := PodIDAndTypesMap(pod)
		if c.err != nil {
			if err == nil {
				t.Errorf("%s, expect err %v, got nil", c.keyWords, c.err)
			} else if err.Error() != c.err.Error() {
				t.Errorf("%s, expect err %v, got %v", c.keyWords, c.err, err)
			}
		}

		if c.idToLabelsMap == nil {
			if len(idToLabelsMap) != 0 {
				t.Errorf("%s, expect idToLabelsMap nil, got %v", c.keyWords, idToLabelsMap)
			}
		} else if !reflect.DeepEqual(idToLabelsMap, c.idToLabelsMap) {
			t.Errorf("%s, expect idToLabelsMap %v, got %v", c.keyWords, c.idToLabelsMap, idToLabelsMap)
		}

		if c.typeToNumsMap == nil {
			if len(typeToNumsMap) != 0 {
				t.Errorf("%s, expect typeToNumsMap nil, got %v", c.keyWords, typeToNumsMap)
			}
		} else if !reflect.DeepEqual(typeToNumsMap, c.typeToNumsMap) {
			t.Errorf("%s, expect typeToNumsMap %v, got %v", c.keyWords, c.typeToNumsMap, typeToNumsMap)
		}
	}
}
