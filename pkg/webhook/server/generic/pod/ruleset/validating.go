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

package ruleset

import (
	"context"
	"encoding/json"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/utils"
)

func (r *RuleSetWebhook) Validating(ctx context.Context, c client.Client, oldPod, newPod *corev1.Pod, operation admissionv1.Operation) error {
	if operation != admissionv1.Update && operation != admissionv1.Create {
		return nil
	}
	if !utils.ControlledByPodOpsLifecycle(newPod) || newPod.Annotations == nil {
		return nil
	}

	ruleSetAnno, ok := newPod.Annotations[appsv1alpha1.AnnotationRuleSets]
	if (operation == admissionv1.Create && !ok) ||
		(operation == admissionv1.Update && ruleSetAnno == oldPod.Annotations[appsv1alpha1.AnnotationRuleSets]) {
		return nil
	}

	rs := &RuleSets{}
	if err := json.Unmarshal([]byte(ruleSetAnno), rs); err != nil {
		return fmt.Errorf("fail to unmarshal RuleSet annotation %s: %v", ruleSetAnno, err)
	}

	return nil
}

type RuleSets []string
