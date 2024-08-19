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

package podtransitionrule

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/kuperator/pkg/utils/inject"
)

func PodHasPodTransitionRuleStage(c client.Client, pod *corev1.Pod, stage string, isDefaultStage bool) (bool, error) {
	rsList := &appsv1alpha1.PodTransitionRuleList{}
	if err := c.List(context.TODO(), rsList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, pod.Name)}); err != nil {
		return false, err
	}
	for _, rs := range rsList.Items {
		for _, rule := range rs.Spec.Rules {
			if rule.Stage != nil && *rule.Stage == stage {
				return true, nil
			}
			if rule.Stage == nil && isDefaultStage {
				return true, nil
			}
		}
	}
	return false, nil
}
