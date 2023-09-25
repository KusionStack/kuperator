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

package rules

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type ManualRuler struct {
	Name string
	Pass bool
}

func (r *ManualRuler) Filter(podTransitionRule *appsv1alpha1.PodTransitionRule, targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	if r.Pass {
		return &FilterResult{Passed: sets.NewString(subjects.List()...), Rejected: map[string]string{}}
	}
	rejectDetails := map[string]string{}
	for podName := range subjects {
		rejectDetails[podName] = "blocked by manual policy, manual rejected"
	}
	return &FilterResult{Passed: sets.NewString(), Rejected: rejectDetails}
}
