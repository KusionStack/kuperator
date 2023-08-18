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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// RuleSetSpec defines the desired state of RuleSet
type RuleSetSpec struct {
	// Selector select the targets controlled by ruleset
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Rules is a set of rules that need to be checked in certain situations
	Rules []RuleSetRule `json:"rules,omitempty"`
}

type RuleSetRule struct {
	// Name is the name of this rule.
	Name string `json:"name,omitempty"`

	// Disabled is the switch to control this rule enable or not.
	// +optional
	Disabled bool `json:"disabled,omitempty"`

	// +optional
	Stage *string `json:"stage,omitempty"`

	// Conditions is the condition to control this rule enable or not.
	// +optional
	Conditions []string `json:"conditions,omitempty"`

	// Filter is used to filter the resource which will be applied with this rule.
	// +optional
	Filter *RuleSetRuleFilter `json:"filter,omitempty"`

	// RuleSetRuleDefinition describes the detail of the rule.
	RuleSetRuleDefinition `json:",inline"`
}

type RuleSetRuleFilter struct {
	// LabelSelector is used to filter resource with label match expresion.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

type RuleSetRuleDefinition struct {

	// AvailablePolicy is the rule to check if the max unavailable number is reached by current resource updated.
	// +optional
	AvailablePolicy *AvailableRule `json:"availablePolicy,omitempty"`

	// LabelCheck is the rule to check labels on pods.
	// +optional
	LabelCheck *LabelCheckRule `json:"labelCheck,omitempty"`

	// +optional
	Webhook *RuleSetRuleWebhook `json:"webhook,omitempty"`
}

type LabelCheckRule struct {
	// Requires is the expected labels on pods
	Requires *metav1.LabelSelector `json:"requires"`
}

type AvailableRule struct {
	// MaxUnavailableValue is the expected max unavailable replicas which is allowed to be a integer or a percentage of the whole
	// number of the target resources.
	MaxUnavailableValue *intstr.IntOrString `json:"maxUnavailableValue,omitempty"`

	// MinAvailableValue is the expected min available replicas which is allowed to be a integer or a percentage of the whole
	// number of the target resources.
	MinAvailableValue *intstr.IntOrString `json:"minAvailableValue,omitempty"`
}

type RuleSetRuleWebhook struct {

	// ClientConfig is the configuration for accessing webhook.
	ClientConfig ClientConfig `json:"clientConfig,omitempty"`

	// FailurePolicy defines how unrecognized errors from the admission endpoint are handled -
	// allowed values are Ignore or Fail. Defaults to Ignore.
	// +optional
	FailurePolicy *FailurePolicyType `json:"failurePolicy,omitempty"`

	// Parameters contains the list of parameters which will be passed in webhook body.
	// +optional
	Parameters []Parameter `json:"parameters,omitempty"`
}

// FailurePolicyType specifies the type of failure policy
type FailurePolicyType string

const (
	// Ignore means that an error calling the webhook is ignored.
	Ignore FailurePolicyType = "Ignore"
	// Fail means that an error calling the webhook causes the admission to fail.
	Fail FailurePolicyType = "Fail"
)
const (
	DefaultWebhookInterval = int64(5)
	DefaultWebhookTimeout  = int64(60)
)

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
	//Type TargetType `json:"type,omitempty"`

	// Selects a field of the pod: supports metadata.name, metadata.namespace, metadata.labels, metadata.annotations,
	// spec.nodeName, spec.serviceAccountName, status.hostIP, status.podIP.
	// +optional
	FieldRef *corev1.ObjectFieldSelector `json:"fieldRef,omitempty"`
}

type ClientConfig struct {
	// `url` gives the location of the webhook, in standard URL form
	// (`scheme://host:port/path`). Exactly one of `url` or `service`
	// must be specified.
	URL string `json:"url"`

	// `caBundle` is a PEM encoded CA bundle which will be used to validate the webhook's server certificate.
	// If unspecified, system trust roots on the apiserver are used. After Base64.
	// +optional
	CABundle string `json:"caBundle,omitempty"`

	// interval give the request time interval, default 5s
	// +optional
	Interval *int64 `json:"interval,omitempty"`

	// timeout give the request time timeout, default 60s
	// +optional
	TraceTimeout *int64 `json:"traceTimeout,omitempty"`
}

// RuleSetStatus defines the observed state of RuleSet
type RuleSetStatus struct {
	UpdateTime *metav1.Time `json:"updateTime,omitempty"`

	// ObservedGeneration is the most recent generation observed for RuleSet
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Targets contains the target resource names this RuleSet is able to select.
	Targets []string `json:"targets,omitempty"`

	// RuleStates contains the RuleState resource info in webhook processing progress.
	// +optional
	RuleStates []*RuleState `json:"ruleStates,omitempty"`

	// Details contains all pods ruleset details
	// +optional
	Details []*Detail `json:"details,omitempty"`
}

// RuleState defines the resource info in webhook processing progress.
type RuleState struct {
	// Name is the name representing the rule
	Name string `json:"name,omitempty"`

	// WebhookStatus is the webhook status representing processing progress
	WebhookStatus *WebhookStatus `json:"webhookStatus,omitempty"`
}

// WebhookStatus defines the webhook processing status
type WebhookStatus struct {
	// RuleSetPodStatus is async request status representing the info of pods
	ItemStatus []*ItemStatus `json:"itemStatus,omitempty"`

	// TraceStates is a list of tracing info
	TraceStates []TraceInfo `json:"traceStates,omitempty"`
}

type TraceInfo struct {
	TraceId string `json:"traceId,omitempty"`

	BeginTime *metav1.Time `json:"beginTime,omitempty"`

	LastTime *metav1.Time `json:"lastTime,omitempty"`

	Message string `json:"message,omitempty"`
}

// ItemStatus defines async request info of resources
type ItemStatus struct {
	// Name representing the name of pod
	Name string `json:"name,omitempty"`

	// WebhookChecked representing the pod has pass check
	WebhookChecked bool `json:"webhookChecked"`

	// TraceId representing async request traceId
	TraceId string `json:"traceId,omitempty"`
}

type Detail struct {
	Name        string       `json:"name,omitempty"`
	Stage       string       `json:"stage,omitempty"`
	Passed      bool         `json:"passed"`
	PassedRules []string     `json:"passedRules,omitempty"`
	RejectInfo  []RejectInfo `json:"rejectInfo,omitempty"`
}

type RejectInfo struct {
	RuleName string `json:"ruleName,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=rs

// RuleSet is the Schema for the rulesets API
type RuleSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RuleSetSpec   `json:"spec,omitempty"`
	Status RuleSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RuleSetList contains a list of RuleSet
type RuleSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RuleSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RuleSet{}, &RuleSetList{})
}
