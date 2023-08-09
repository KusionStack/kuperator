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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

func TestValidating(t *testing.T) {
	inputs := []struct {
		labels   map[string]string
		keyWords string // used to check the error message
	}{
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"): "1402144848",
			},
			keyWords: fmt.Sprintf("not found label %s", fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123")),
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			keyWords: fmt.Sprintf("not found label %s", fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123")),
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"): "1402144848",
			},
			keyWords: fmt.Sprintf("not found label %s", fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456")),
		},

		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "true",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",
			},
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "true",
			},
			keyWords: v1alpha1.PodOperationPermissionLabelPrefix,
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",
			},
			keyWords: v1alpha1.PodPreCheckLabelPrefix,
		},
	}

	lifecycle := &OpsLifecycle{}
	for _, v := range inputs {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: v.labels,
			},
		}

		err := lifecycle.Validating(context.Background(), pod, nil)
		if v.keyWords == "" {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), v.keyWords)
		}
	}
}

func TestMutating(t *testing.T) {
	inputs := []struct {
		notes          string
		oldPodLabels   map[string]string
		newPodLabels   map[string]string
		expectedLabels map[string]string

		readyToUpgrade ReadyToUpgrade

		keyWords string // used to check the error message
	}{
		{
			notes: "invalid label",
			newPodLabels: map[string]string{
				v1alpha1.PodOperatingLabelPrefix:                                  "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			keyWords: fmt.Sprintf("invalid label %s", v1alpha1.PodOperatingLabelPrefix),
		},
		{
			notes: "invalid label",
			newPodLabels: map[string]string{
				v1alpha1.PodPreCheckLabelPrefix:                                   "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			keyWords: fmt.Sprintf("invalid label %s", v1alpha1.PodPreCheckLabelPrefix),
		},

		{
			notes: "pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "1402144848",
			},
		},
		{
			notes: "pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "456"): "1402144848",
			},
		},

		{
			notes: "prepare",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, "123"): "1402144848",
			},

			readyToUpgrade: readyToUpgradeReturnFalse,
		},

		{
			notes: "prepare, undo",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, "123"): "upgrade",
			},
			expectedLabels: map[string]string{},
		},

		{
			notes: "prepare, operate, undo",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"):           "replace",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "456"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "456"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "replace"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"):           "replace",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "456"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "456"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "replace"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, "456"): "1402144848",
			},
			readyToUpgrade: readyToUpgradeReturnFalse,
		},

		{
			notes: "prepare, pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "replace",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "replace",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "456"): "1402144848",
			},

			readyToUpgrade: readyToUpgradeReturnFalse,
		},

		{
			notes: "prepare, operate",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, "123"): "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"): "1402144848",
			},
		},

		{
			notes: "operated",
			oldPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):                 "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, "123"):                 "1402144848",
			},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, "upgrade"): "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, "123"):                 "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPrepareLabelPrefix, "123"):                 "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
			},
		},

		{
			notes: "post-check, but wait for operating",
			oldPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "456"): "upgrade",
			},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckLabelPrefix, "123"): "1402144848",
			},
		},
		{
			notes: "post-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "456"): "1402144848",
			},
		},

		{
			notes: "complete",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "456"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "456"):       "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "456"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "456"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, "456"): "1402144848",
			},
		},

		{
			notes: "all finished",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodOperatedLabelPrefix, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodDoneOperationTypeLabelPrefix, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckLabelPrefix, "456"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, "456"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.PodCompleteLabelPrefix, "456"): "1402144848",
			},
			expectedLabels: map[string]string{v1alpha1.PodServiceAvailableLabel: "1402144848"},
		},
	}

	opslifecycle := &OpsLifecycle{
		timeLabelValue: func() string {
			return "1402144848"
		},
	}
	for _, v := range inputs {
		oldPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "old",
				Namespace: "kafed",
				Labels:    v.oldPodLabels,
			},
		}
		newPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "new",
				Namespace: "kafed",
				Labels:    v.newPodLabels,
			},
		}

		opslifecycle.readyToUpgrade = v.readyToUpgrade
		if opslifecycle.readyToUpgrade == nil {
			opslifecycle.readyToUpgrade = readyToUpgradeReturnTrue
		}

		t.Logf("notes: %s", v.notes)
		err := opslifecycle.Mutating(context.Background(), oldPod, newPod, nil, nil)
		if v.keyWords == "" {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), v.keyWords)
			continue
		}
		assert.Equal(t, v.expectedLabels, newPod.Labels)
	}
}

func readyToUpgradeReturnTrue(pod *corev1.Pod) (bool, []string, *time.Duration) {
	return true, nil, nil
}

func readyToUpgradeReturnFalse(pod *corev1.Pod) (bool, []string, *time.Duration) {
	return false, nil, nil
}
