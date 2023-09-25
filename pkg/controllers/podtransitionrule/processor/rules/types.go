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

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
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

func GetRuler(rule *appsv1alpha1.PodTransitionRuleRule, client client.Client) Ruler {

	if rule.AvailablePolicy != nil {
		return &AvailableRuler{
			Client:              client,
			MinAvailableValue:   rule.AvailablePolicy.MinAvailableValue,
			MaxUnavailableValue: rule.AvailablePolicy.MaxUnavailableValue,
			Name:                rule.Name,
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

type Response struct {
	Success      bool     `json:"success"`
	Message      string   `json:"message"`
	RetryByTrace bool     `json:"retryByTrace"`
	Passed       []string `json:"finishedNames,omitempty"`
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

// WebhookReq is representing the request parameter.
type WebhookReq struct {
	TraceId string `json:"traceId,omitempty"`

	RetryByTrace bool `json:"retryByTrace,omitempty"`

	RuleName string `json:"ruleName,omitempty"`

	Stage *string `json:"stage,omitempty"`

	// Resources contains the list of resource parameter
	Resources []ResourceParameter `json:"resources,omitempty"`

	Parameters map[string]string `json:"parameters,omitempty"`
}

// ResourceParameter is representing the request body of resource parameter
type ResourceParameter struct {
	// APIVersion defines the versioned schema of this representation of an object.
	ApiVersion string `json:"apiVersion"`

	// Kind is a string value representing the REST resource this object represents.
	Kind string `json:"kind"`

	// Name is a string value representing resource name
	Name string `json:"name,omitempty"`

	// Parameters is a string map representing parameters
	Parameters map[string]string `json:"parameters,omitempty"`
}

type Parameter struct {
	// Key is the parameter key.
	Key string `json:"key,omitempty"`

	// Value is the string value of this parameter.
	// Defaults to "".
	// +optional
	Value string `json:"value,omitempty"`

	// Source for the parameter's value. Cannot be used if value is not empty.
	// +optional
	ValueFrom *ParameterSource `json:"valueFrom,omitempty"`
}

type ParameterSource struct {

	// Type defines target pod type.
	// +optional
	Type string `json:"type,omitempty"`

	// Selects a field of the pod: supports metadata.name, metadata.namespace, metadata.labels, metadata.annotations,
	// spec.nodeName, spec.serviceAccountName, status.hostIP, status.podIP.
	// +optional
	FieldRef *corev1.ObjectFieldSelector `json:"fieldRef,omitempty"`
}

func (w *Webhook) buildRequest(pods sets.String, oldTrace *string) (*WebhookReq, error) {

	req := &WebhookReq{
		RuleName: w.RuleName,
		Stage:    w.Stage,
	}
	if oldTrace != nil {
		req.TraceId = *oldTrace
		req.RetryByTrace = true
		return req, nil
	}
	webhookPodsParameters := make([]ResourceParameter, 0, pods.Len())
	for podName := range pods {
		podPara := ResourceParameter{
			ApiVersion: "core/v1",
			Kind:       "Pod",
			Name:       podName,
		}
		parameters := map[string]string{}
		for _, parameter := range w.Webhook.Parameters {
			value, err := w.parseParameter(&parameter, w.targets[podName])
			if err != nil {
				return nil, fmt.Errorf("%s failed to parse parameter, %v", w.key(), err)
			}
			parameters[parameter.Key] = value
		}
		podPara.Parameters = parameters
		webhookPodsParameters = append(webhookPodsParameters, podPara)
	}
	req.TraceId = NewTrace()
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
