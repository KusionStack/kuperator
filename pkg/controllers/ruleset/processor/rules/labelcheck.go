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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
)

type LabelCheckRuler struct {
	Name     string
	Selector *metav1.LabelSelector
}

func (l *LabelCheckRuler) Filter(ruleSet *appsv1alpha1.RuleSet, targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	passed := sets.NewString()
	rejected := map[string]string{}
	sel, err := metav1.LabelSelectorAsSelector(l.Selector)
	if err != nil {
		return rejectAllWithErr(subjects, passed, rejected, fmt.Sprintf("labelCheck error: %v", err))
	}

	for podName := range subjects {
		pod := targets[podName]
		if sel.Matches(labels.Set(pod.Labels)) {
			passed.Insert(podName)
		} else {
			rejected[podName] = fmt.Sprintf("block by label check policy, pod %s/%s labels not match %s", pod.Namespace, pod.Name, sel.String())
		}
	}
	klog.Infof("finish do label check, passed: %d, rejected: %d", len(passed), len(rejected))
	return &FilterResult{Passed: passed, Rejected: rejected}
}
