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
		labels map[string]string
		err    error
	}{
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",
			},
			err: nil,
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"): "1402144848",
			},
			err: fmt.Errorf("not found label %s", fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123")),
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",
			},
			err: fmt.Errorf("not found label %s", fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123")),
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"): "1402144848",
			},
			err: fmt.Errorf("not found label %s", fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456")),
		},

		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "true",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",
			},
			err: nil,
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"): "true",
			},
			err: fmt.Errorf("not found labels %s", "map[operation-permission.kafed.kusionstack.io:{}]"),
		},
		{
			labels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",
			},
			err: fmt.Errorf("not found labels %s", "map[pre-check.lifecycle.kafed.kusionstack.io:{}]"),
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
		assert.Equal(t, v.err, err)
	}
}

func TestMutating(t *testing.T) {
	inputs := []struct {
		notes          string
		oldPodLabels   map[string]string
		newPodLabels   map[string]string
		expectedLabels map[string]string

		readyToUpgrade            ReadyToUpgrade
		satisfyExpectedFinalizers SatisfyExpectedFinalizers

		err error
	}{
		{
			notes: "invalid label",
			newPodLabels: map[string]string{
				v1alpha1.LabelPodOperating:                                  "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",
			},
			err: fmt.Errorf("invalid label %s", v1alpha1.LabelPodOperating),
		},
		{
			notes: "invalid label",
			newPodLabels: map[string]string{
				v1alpha1.LabelPodPreCheck:                                   "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",
			},
			err: fmt.Errorf("invalid label %s", v1alpha1.LabelPodPreCheck),
		},

		{
			notes: "pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"): "1402144848",
			},
			err: nil,
		},
		{
			notes: "pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "456"): "1402144848",
			},
			err: nil,
		},

		{
			notes: "prepare",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, "123"): "1402144848",
			},

			readyToUpgrade: readyToUpgradeReturnFalse,
			err:            nil,
		},

		{
			notes: "prepare, undo",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodUndoOperationType, "123"): "upgrade",
			},
			expectedLabels: map[string]string{},
			err:            nil,
		},

		{
			notes: "prepare, operate, undo",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodUndoOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"):           "replace",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "456"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "456"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "replace"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"):           "replace",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "456"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "456"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "replace"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, "456"): "1402144848",
			},
			readyToUpgrade: readyToUpgradeReturnFalse,
			err:            nil,
		},

		{
			notes: "prepare, pre-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"): "replace",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"): "replace",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "456"): "1402144848",
			},

			readyToUpgrade: readyToUpgradeReturnFalse,
			err:            nil,
		},

		{
			notes: "prepare, operate",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):               "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"):           "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, "123"): "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperate, "123"): "1402144848",
			},

			err: nil,
		},

		{
			notes: "operated",
			oldPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperate, "123"):                 "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, "123"):                 "1402144848",
			},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"):                "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreChecked, "123"):              "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationPermission, "upgrade"): "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperate, "123"):                 "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPrepare, "123"):                 "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",
			},
			err: nil,
		},

		{
			notes: "post-check, but wait for operating",
			oldPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "456"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "456"): "upgrade",
			},
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperating, "123"):     "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPreCheck, "123"): "1402144848",
			},
			err: nil,
		},
		{
			notes: "post-check",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "456"): "upgrade",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "456"): "upgrade",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "456"): "1402144848",
			},
			err: nil,
		},

		{
			notes: "complete",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "456"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "456"):       "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "456"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "456"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, "456"): "1402144848",
			},
			err: nil,
		},

		{
			notes: "wait for removing finalizers",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, "123"): "1402144848",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, "123"): "1402144848",
			},
			satisfyExpectedFinalizers: satifyExpectedFinalizersReturnFalse,
			err:                       nil,
		},
		{
			notes: "all finished",
			newPodLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "123"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "123"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "123"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "123"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, "123"): "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodOperated, "456"):          "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodDoneOperationType, "456"): "upgrade",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostCheck, "456"):         "1402144848",
				fmt.Sprintf("%s/%s", v1alpha1.LabelPodPostChecked, "456"):       "1402144848",

				fmt.Sprintf("%s/%s", v1alpha1.LabelPodComplete, "456"): "1402144848",
			},
			expectedLabels: map[string]string{},
			err:            nil,
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
		opslifecycle.satisfyExpectedFinalizers = v.satisfyExpectedFinalizers
		if opslifecycle.satisfyExpectedFinalizers == nil {
			opslifecycle.satisfyExpectedFinalizers = satifyExpectedFinalizersReturnTrue
		}

		t.Logf("notes: %s", v.notes)
		err := opslifecycle.Mutating(context.Background(), oldPod, newPod, nil, nil)
		assert.Equal(t, v.err, err)
		if v.err != nil {
			continue
		}
		assert.Equal(t, v.expectedLabels, newPod.Labels)
	}
}

func readyToUpgradeReturnTrue(pod *corev1.Pod) (bool, []string, *time.Duration) {
	return true, nil, nil
}

func satifyExpectedFinalizersReturnTrue(pod *corev1.Pod) (bool, []string, error) {
	return true, nil, nil
}

func readyToUpgradeReturnFalse(pod *corev1.Pod) (bool, []string, *time.Duration) {
	return false, nil, nil
}

func satifyExpectedFinalizersReturnFalse(pod *corev1.Pod) (bool, []string, error) {
	return false, nil, nil
}
