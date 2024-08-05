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

package opslifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kusionstack.io/kube-api/apps/v1alpha1"
)

func TestValidating(t *testing.T) {
	inputs := []struct {
		labels   map[string]string
		keyWords string // Used to check the error message
	}{
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"): "1717505885197871195",
			},
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"): "1717505885197871195",
			},
		},

		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "true",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
			},
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"): "true",
			},
			keyWords: v1alpha1.PodOperationPermissionLabelPrefix,
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
			},
			keyWords: v1alpha1.PodPreCheckedLabelPrefix,
		},
		{
			labels: map[string]string{
				v1alpha1.PodOperatingLabelPrefix: "1717505885197871195",
			},
			keyWords: "invalid label",
		},
		{
			labels: map[string]string{
				v1alpha1.PodOperationTypeLabelPrefix: "1717505885197871195",
			},
			keyWords: "invalid label",
		},
	}

	lifecycle := &OpsLifecycle{}
	for _, v := range inputs {
		if v.labels != nil {
			v.labels[v1alpha1.ControlledByKusionStackLabelKey] = "true"
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test1",
				Namespace: "default",
				Labels:    v.labels,
			},
		}

		err := lifecycle.Validating(context.Background(), nil, nil, pod, admissionv1.Update)
		if v.keyWords == "" {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), v.keyWords)
		}
	}
}

func TestMutating(t *testing.T) {
	expectedFinalizer := "finalizer1"
	availableConditions := &v1alpha1.PodAvailableConditions{
		ExpectedFinalizers: map[string]string{
			"a.b.c/1": expectedFinalizer,
		},
	}
	s, _ := json.Marshal(availableConditions)
	availableConditionsAnnotationVal := string(s)

	inputs := []struct {
		note     string
		keyWords string // Used to check the error message

		oldPodLabels      map[string]string
		newPodLabels      map[string]string
		newPodAnnotations map[string]string
		newPodFinalizers  []string
		expectedLabels    map[string]string

		readyToOperate ReadyToOperate
	}{
		{
			note: "pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "1717505885197871195",
			},
		},
		{
			note: "pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "456"): "1717505885197871195",
			},
		},

		{
			note: "preparing",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreparingLabelPrefix, "123"): "1717505885197871195",
			},

			readyToOperate: readyToUpgradeReturnFalse,
		},

		{
			note: "preparing, undo",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, "123"): "upgrade",
			},
			expectedLabels: map[string]string{},
		},

		{
			note: "preparing, operate, undo",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"):           "replace",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "456"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "replace"): "1717505885197871195",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"):           "replace",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "456"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "replace"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreparingLabelPrefix, "456"): "1717505885197871195",
			},
			readyToOperate: readyToUpgradeReturnFalse,
		},

		{
			note: "preparing, pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "replace",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreparingLabelPrefix, "123"):               "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "replace",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "456"):      "1717505885197871195",
			},

			readyToOperate: readyToUpgradeReturnFalse,
		},

		{
			note: "preparing, operate",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreparingLabelPrefix, "123"): "1717505885197871195",
			},
			readyToOperate: readyToUpgradeReturnFalse,
		},

		{
			note: "preparing, operate",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "restart",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "restart"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"):          "delete",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "456"):             "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "delete"): "1717505885197871195",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "restart",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "restart"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"):          "delete",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "456"):             "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "delete"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"): "1717505885197871195",
			},
		},

		{
			note: "operated",
			oldPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):                 "1717505885197871195",
			},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):                 "1717505885197871195",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"):         "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
			},
		},

		{
			note: "post-check, but wait for operating",
			oldPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "upgrade",
			},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "1717505885197871195",
			},
		},

		{
			note: "post-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "456"): "1717505885197871195",
			},
		},

		{
			note: "complete",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "456"):       "1717505885197871195",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "123"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "456"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "456"): "1717505885197871195",
			},
		},

		{
			note: "all expected finalizers satisfied",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "123"): "1717505885197871195",
			},
			expectedLabels: map[string]string{},
		},

		{
			note:             "all expected finalizers satisfied",
			newPodFinalizers: []string{expectedFinalizer},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "123"): "1717505885197871195",
			},
			newPodAnnotations: map[string]string{
				v1alpha1.PodAvailableConditionsAnnotation: availableConditionsAnnotationVal,
			},
			expectedLabels: map[string]string{},
		},

		{
			note:             "not all expected finalizers satisfied",
			newPodFinalizers: []string{},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "123"): "1717505885197871195",
			},
			newPodAnnotations: map[string]string{
				v1alpha1.PodAvailableConditionsAnnotation: availableConditionsAnnotationVal,
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "123"): "1717505885197871195",
			},
		},

		{
			note: "all finished",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"):         "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "123"): "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "456"):           "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "456"):         "1717505885197871195",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "456"):       "1717505885197871195",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompletingLabelPrefix, "456"): "1717505885197871195",
			},
			expectedLabels: map[string]string{},
		},
	}

	for _, v := range inputs {
		if v.oldPodLabels != nil {
			v.oldPodLabels[v1alpha1.ControlledByKusionStackLabelKey] = "true"
		}
		oldPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "old",
				Namespace: "operating",
				Labels:    v.oldPodLabels,
			},
		}

		if v.newPodLabels != nil {
			v.newPodLabels[v1alpha1.ControlledByKusionStackLabelKey] = "true"
		}
		newPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "new",
				Namespace:   "operating",
				Labels:      v.newPodLabels,
				Annotations: v.newPodAnnotations,
				Finalizers:  v.newPodFinalizers,
			},
		}

		opslifecycle := getOpsLifecycleWithFuncs(v.readyToOperate)

		t.Logf("note: %s", v.note)
		err := opslifecycle.Mutating(context.Background(), nil, oldPod, newPod, admissionv1.Update)
		if v.keyWords == "" {
			assert.Nil(t, err)
		} else {
			if assert.NotNil(t, err) {
				assert.Contains(t, err.Error(), v.keyWords)
			}
			continue
		}

		if v.expectedLabels != nil {
			v.expectedLabels[v1alpha1.ControlledByKusionStackLabelKey] = "true"
		}
		assert.Equal(t, v.expectedLabels, newPod.Labels)
	}
}

func TestReadyToUpgrade(t *testing.T) {
	inputs := []struct {
		note string

		podStatus      corev1.PodStatus
		finalizers     []string
		readyToOperate bool
	}{
		{
			note: "no protection finalizer, and no expected conditions",
			podStatus: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   "pod.kusionstack.io/test",
						Status: corev1.ConditionTrue,
					},
				},
			},
			readyToOperate: true,
		},
		{
			note: "no protection finalizer, and service-ready is true",
			podStatus: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   v1alpha1.ReadinessGatePodServiceReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			readyToOperate: false,
		},
		{
			note: "no protection finalizer, and service-ready is false",
			podStatus: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   v1alpha1.ReadinessGatePodServiceReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			readyToOperate: true,
		},
		{
			note: "has protection finalizer, and service-ready is false",
			podStatus: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   v1alpha1.ReadinessGatePodServiceReady,
						Status: corev1.ConditionFalse,
					},
				},
			},
			finalizers:     []string{fmt.Sprintf("%s/%s", v1alpha1.PodOperationProtectionFinalizerPrefix, "finalizer1")},
			readyToOperate: false,
		},
		{
			note: "has protection finalizer, and service-ready is true",
			podStatus: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   v1alpha1.ReadinessGatePodServiceReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
			finalizers:     []string{fmt.Sprintf("%s/%s", v1alpha1.PodOperationProtectionFinalizerPrefix, "finalizer1")},
			readyToOperate: false,
		},
	}
	for _, v := range inputs {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test1",
				Namespace:  "default",
				Finalizers: v.finalizers,
			},
			Spec:   corev1.PodSpec{},
			Status: v.podStatus,
		}

		if assert.Equal(t, v.readyToOperate, readyToOperate(pod)) {
			t.Logf("note: %s", v.note)
		}
	}
}

func getOpsLifecycleWithFuncs(readyToOperate ReadyToOperate) *OpsLifecycle {
	opslifecycle := &OpsLifecycle{
		timeLabelValue: func() string {
			return "1717505885197871195"
		},
		readyToOperate: readyToUpgradeReturnTrue,
	}

	if readyToOperate != nil {
		opslifecycle.readyToOperate = readyToOperate
	}
	return opslifecycle
}

func readyToUpgradeReturnTrue(pod *corev1.Pod) bool {
	return true
}

func readyToUpgradeReturnFalse(pod *corev1.Pod) bool {
	return false
}
