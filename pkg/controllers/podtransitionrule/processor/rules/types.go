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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

type Ruler interface {
	Filter(podTransitionRule *appsv1alpha1.PodTransitionRule, targets map[string]*corev1.Pod, subjects sets.String) *FilterResult
}

type FilterResult struct {
	Passed   sets.String
	Rejected map[string]string
	Interval *time.Duration
	Err      error

	RuleState *appsv1alpha1.RuleState
}

func GetRuler(rule *appsv1alpha1.TransitionRule, client client.Client) Ruler {

	if rule.AvailablePolicy != nil {
		return &AvailableRuler{
			Client:               client,
			MinAvailableValue:    rule.AvailablePolicy.MinAvailableValue,
			MaxUnavailableValue:  rule.AvailablePolicy.MaxUnavailableValue,
			MinAvailablePolicy:   rule.AvailablePolicy.MinAvailablePolicy,
			MaxUnavailablePolicy: rule.AvailablePolicy.MaxUnavailablePolicy,
			Name:                 rule.Name,
		}
	}
	if rule.LabelCheck != nil {
		return &LabelCheckRuler{
			Name:     rule.Name,
			Selector: rule.LabelCheck.Requires,
		}
	}
	if rule.Webhook != nil {
		return &WebhookRuler{Name: rule.Name}
	}
	return nil
}

func rejectAllWithErr(subjects, passed sets.String, rejects map[string]string, format string, a ...any) *FilterResult {
	reject(subjects, passed, rejects, fmt.Sprintf(format, a...))
	return &FilterResult{Passed: passed, Rejected: rejects, Err: fmt.Errorf(format, a...)}
}

func reject(subjects, passed sets.String, rejects map[string]string, reason string) {
	for item := range subjects {
		if passed.Has(item) {
			continue
		}
		if _, ok := rejects[item]; !ok {
			rejects[item] = reason
		}
	}
}

type HttpJob struct {
	Url      string
	CaBundle []byte

	Timeout  int32
	Interval int32

	Body    interface{}
	TraceId string

	Action string
}

func (w *Webhook) buildRequest(pods sets.String, targets map[string]*corev1.Pod) (*appsv1alpha1.WebhookRequest, error) {

	req := &appsv1alpha1.WebhookRequest{
		RuleName: w.RuleName,
		Stage:    w.Stage,
		TraceId:  NewTrace(),
	}
	webhookPodsParameters := make([]appsv1alpha1.ResourceParameter, 0, pods.Len())
	for podName := range pods {
		podPara := appsv1alpha1.ResourceParameter{
			ApiVersion: "core/v1",
			Kind:       "Pod",
			Name:       podName,
		}
		parameters := map[string]string{}
		for _, parameter := range w.Webhook.Parameters {
			value, err := w.parseParameter(&parameter, targets[podName])
			if err != nil {
				return nil, fmt.Errorf("%s failed to parse parameter, %v", w.Key, err)
			}
			parameters[parameter.Key] = value
		}
		podPara.Parameters = parameters
		webhookPodsParameters = append(webhookPodsParameters, podPara)
	}
	req.Resources = webhookPodsParameters
	return req, nil
}

func (w *Webhook) parseParameter(parameter *appsv1alpha1.Parameter, pod *corev1.Pod) (value string, err error) {

	defer func() {
		if value == "null" {
			value = ""
		}
	}()

	if len(parameter.Value) != 0 {
		return parameter.Value, nil
	}
	if parameter.ValueFrom == nil || parameter.ValueFrom.FieldRef == nil {
		return "", fmt.Errorf("unexpected empty parameter %s", parameter.Key)
	}

	return ExtractValueFromPod(pod, parameter.Key, parameter.ValueFrom.FieldRef.FieldPath)
}
