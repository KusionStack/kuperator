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
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/utils"
)

func (lc *OpsLifecycle) Validating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	if operation == admissionv1.Delete || !utils.ControlledByKusionStack(newPod) {
		return nil
	}

	if _, err := controllerutils.PodAvailableConditions(newPod); err != nil {
		return err
	}

	expectedLabels := make(map[string]struct{})
	foundLabels := make(map[string]struct{})
	for label := range newPod.Labels {
		for _, v := range pairLabelPrefixesMap { // labels must exist together and have the same id
			if !strings.HasPrefix(label, v) {
				continue
			}

			s := strings.Split(label, "/")
			if len(s) != 2 {
				return fmt.Errorf("invalid label %s", label)
			}
			id := s[1]

			if id != "" {
				pairLabel := fmt.Sprintf("%s/%s", pairLabelPrefixesMap[v], id)
				_, ok := newPod.Labels[pairLabel]
				if !ok {
					return fmt.Errorf("not found label %s", pairLabel)
				}
			}
		}

		found := false
		for v := range expectedLabels { // try to find the expected another label prefixes
			if strings.HasPrefix(label, v) {
				foundLabels[v] = struct{}{}
				found = true
				break
			}
		}
		if found {
			continue
		}

		for _, v := range coexistingLabelPrefixesMap { // labels must exist together
			if !strings.HasPrefix(label, v) {
				continue
			}
			expectedLabels[coexistingLabelPrefixesMap[v]] = struct{}{}
		}
	}

	if len(expectedLabels) != len(foundLabels) {
		return fmt.Errorf("not found the expected label prefixes: %v", expectedLabels)
	}
	return nil
}
