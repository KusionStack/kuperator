//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AvailableRule) DeepCopyInto(out *AvailableRule) {
	*out = *in
	if in.MaxUnavailableValue != nil {
		in, out := &in.MaxUnavailableValue, &out.MaxUnavailableValue
		*out = new(intstr.IntOrString)
		**out = **in
	}
	if in.MinAvailableValue != nil {
		in, out := &in.MinAvailableValue, &out.MinAvailableValue
		*out = new(intstr.IntOrString)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AvailableRule.
func (in *AvailableRule) DeepCopy() *AvailableRule {
	if in == nil {
		return nil
	}
	out := new(AvailableRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ByLabel) DeepCopyInto(out *ByLabel) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ByLabel.
func (in *ByLabel) DeepCopy() *ByLabel {
	if in == nil {
		return nil
	}
	out := new(ByLabel)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ByPartition) DeepCopyInto(out *ByPartition) {
	*out = *in
	if in.Partition != nil {
		in, out := &in.Partition, &out.Partition
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ByPartition.
func (in *ByPartition) DeepCopy() *ByPartition {
	if in == nil {
		return nil
	}
	out := new(ByPartition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ClientConfigBeta1) DeepCopyInto(out *ClientConfigBeta1) {
	*out = *in
	if in.Poll != nil {
		in, out := &in.Poll, &out.Poll
		*out = new(Poll)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ClientConfigBeta1.
func (in *ClientConfigBeta1) DeepCopy() *ClientConfigBeta1 {
	if in == nil {
		return nil
	}
	out := new(ClientConfigBeta1)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CollaSet) DeepCopyInto(out *CollaSet) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CollaSet.
func (in *CollaSet) DeepCopy() *CollaSet {
	if in == nil {
		return nil
	}
	out := new(CollaSet)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CollaSet) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CollaSetCondition) DeepCopyInto(out *CollaSetCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CollaSetCondition.
func (in *CollaSetCondition) DeepCopy() *CollaSetCondition {
	if in == nil {
		return nil
	}
	out := new(CollaSetCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CollaSetList) DeepCopyInto(out *CollaSetList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CollaSet, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CollaSetList.
func (in *CollaSetList) DeepCopy() *CollaSetList {
	if in == nil {
		return nil
	}
	out := new(CollaSetList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CollaSetList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CollaSetSpec) DeepCopyInto(out *CollaSetSpec) {
	*out = *in
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	in.Template.DeepCopyInto(&out.Template)
	if in.VolumeClaimTemplates != nil {
		in, out := &in.VolumeClaimTemplates, &out.VolumeClaimTemplates
		*out = make([]corev1.PersistentVolumeClaim, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.UpdateStrategy.DeepCopyInto(&out.UpdateStrategy)
	in.ScaleStrategy.DeepCopyInto(&out.ScaleStrategy)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CollaSetSpec.
func (in *CollaSetSpec) DeepCopy() *CollaSetSpec {
	if in == nil {
		return nil
	}
	out := new(CollaSetSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CollaSetStatus) DeepCopyInto(out *CollaSetStatus) {
	*out = *in
	if in.CollisionCount != nil {
		in, out := &in.CollisionCount, &out.CollisionCount
		*out = new(int32)
		**out = **in
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]CollaSetCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CollaSetStatus.
func (in *CollaSetStatus) DeepCopy() *CollaSetStatus {
	if in == nil {
		return nil
	}
	out := new(CollaSetStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ContextDetail) DeepCopyInto(out *ContextDetail) {
	*out = *in
	if in.Data != nil {
		in, out := &in.Data, &out.Data
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ContextDetail.
func (in *ContextDetail) DeepCopy() *ContextDetail {
	if in == nil {
		return nil
	}
	out := new(ContextDetail)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LabelCheckRule) DeepCopyInto(out *LabelCheckRule) {
	*out = *in
	if in.Requires != nil {
		in, out := &in.Requires, &out.Requires
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LabelCheckRule.
func (in *LabelCheckRule) DeepCopy() *LabelCheckRule {
	if in == nil {
		return nil
	}
	out := new(LabelCheckRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Parameter) DeepCopyInto(out *Parameter) {
	*out = *in
	if in.ValueFrom != nil {
		in, out := &in.ValueFrom, &out.ValueFrom
		*out = new(ParameterSource)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Parameter.
func (in *Parameter) DeepCopy() *Parameter {
	if in == nil {
		return nil
	}
	out := new(Parameter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ParameterSource) DeepCopyInto(out *ParameterSource) {
	*out = *in
	if in.FieldRef != nil {
		in, out := &in.FieldRef, &out.FieldRef
		*out = new(corev1.ObjectFieldSelector)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ParameterSource.
func (in *ParameterSource) DeepCopy() *ParameterSource {
	if in == nil {
		return nil
	}
	out := new(ParameterSource)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PersistentVolumeClaimRetentionPolicy) DeepCopyInto(out *PersistentVolumeClaimRetentionPolicy) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PersistentVolumeClaimRetentionPolicy.
func (in *PersistentVolumeClaimRetentionPolicy) DeepCopy() *PersistentVolumeClaimRetentionPolicy {
	if in == nil {
		return nil
	}
	out := new(PersistentVolumeClaimRetentionPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodTransitionDetail) DeepCopyInto(out *PodTransitionDetail) {
	*out = *in
	if in.PassedRules != nil {
		in, out := &in.PassedRules, &out.PassedRules
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.RejectInfo != nil {
		in, out := &in.RejectInfo, &out.RejectInfo
		*out = make([]RejectInfo, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodTransitionDetail.
func (in *PodTransitionDetail) DeepCopy() *PodTransitionDetail {
	if in == nil {
		return nil
	}
	out := new(PodTransitionDetail)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodTransitionRule) DeepCopyInto(out *PodTransitionRule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodTransitionRule.
func (in *PodTransitionRule) DeepCopy() *PodTransitionRule {
	if in == nil {
		return nil
	}
	out := new(PodTransitionRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PodTransitionRule) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodTransitionRuleList) DeepCopyInto(out *PodTransitionRuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PodTransitionRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodTransitionRuleList.
func (in *PodTransitionRuleList) DeepCopy() *PodTransitionRuleList {
	if in == nil {
		return nil
	}
	out := new(PodTransitionRuleList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PodTransitionRuleList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodTransitionRuleSpec) DeepCopyInto(out *PodTransitionRuleSpec) {
	*out = *in
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.Rules != nil {
		in, out := &in.Rules, &out.Rules
		*out = make([]TransitionRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodTransitionRuleSpec.
func (in *PodTransitionRuleSpec) DeepCopy() *PodTransitionRuleSpec {
	if in == nil {
		return nil
	}
	out := new(PodTransitionRuleSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodTransitionRuleStatus) DeepCopyInto(out *PodTransitionRuleStatus) {
	*out = *in
	if in.UpdateTime != nil {
		in, out := &in.UpdateTime, &out.UpdateTime
		*out = (*in).DeepCopy()
	}
	if in.Targets != nil {
		in, out := &in.Targets, &out.Targets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.RuleStates != nil {
		in, out := &in.RuleStates, &out.RuleStates
		*out = make([]*RuleState, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(RuleState)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	if in.Details != nil {
		in, out := &in.Details, &out.Details
		*out = make([]*PodTransitionDetail, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(PodTransitionDetail)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodTransitionRuleStatus.
func (in *PodTransitionRuleStatus) DeepCopy() *PodTransitionRuleStatus {
	if in == nil {
		return nil
	}
	out := new(PodTransitionRuleStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Poll) DeepCopyInto(out *Poll) {
	*out = *in
	if in.IntervalSeconds != nil {
		in, out := &in.IntervalSeconds, &out.IntervalSeconds
		*out = new(int64)
		**out = **in
	}
	if in.TimeoutSeconds != nil {
		in, out := &in.TimeoutSeconds, &out.TimeoutSeconds
		*out = new(int64)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Poll.
func (in *Poll) DeepCopy() *Poll {
	if in == nil {
		return nil
	}
	out := new(Poll)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PollResponse) DeepCopyInto(out *PollResponse) {
	*out = *in
	if in.FinishedNames != nil {
		in, out := &in.FinishedNames, &out.FinishedNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PollResponse.
func (in *PollResponse) DeepCopy() *PollResponse {
	if in == nil {
		return nil
	}
	out := new(PollResponse)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RejectInfo) DeepCopyInto(out *RejectInfo) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RejectInfo.
func (in *RejectInfo) DeepCopy() *RejectInfo {
	if in == nil {
		return nil
	}
	out := new(RejectInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceContext) DeepCopyInto(out *ResourceContext) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceContext.
func (in *ResourceContext) DeepCopy() *ResourceContext {
	if in == nil {
		return nil
	}
	out := new(ResourceContext)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ResourceContext) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceContextList) DeepCopyInto(out *ResourceContextList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ResourceContext, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceContextList.
func (in *ResourceContextList) DeepCopy() *ResourceContextList {
	if in == nil {
		return nil
	}
	out := new(ResourceContextList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ResourceContextList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceContextSpec) DeepCopyInto(out *ResourceContextSpec) {
	*out = *in
	if in.Contexts != nil {
		in, out := &in.Contexts, &out.Contexts
		*out = make([]ContextDetail, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceContextSpec.
func (in *ResourceContextSpec) DeepCopy() *ResourceContextSpec {
	if in == nil {
		return nil
	}
	out := new(ResourceContextSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ResourceParameter) DeepCopyInto(out *ResourceParameter) {
	*out = *in
	if in.Parameters != nil {
		in, out := &in.Parameters, &out.Parameters
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ResourceParameter.
func (in *ResourceParameter) DeepCopy() *ResourceParameter {
	if in == nil {
		return nil
	}
	out := new(ResourceParameter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RollingUpdateCollaSetStrategy) DeepCopyInto(out *RollingUpdateCollaSetStrategy) {
	*out = *in
	if in.ByPartition != nil {
		in, out := &in.ByPartition, &out.ByPartition
		*out = new(ByPartition)
		(*in).DeepCopyInto(*out)
	}
	if in.ByLabel != nil {
		in, out := &in.ByLabel, &out.ByLabel
		*out = new(ByLabel)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RollingUpdateCollaSetStrategy.
func (in *RollingUpdateCollaSetStrategy) DeepCopy() *RollingUpdateCollaSetStrategy {
	if in == nil {
		return nil
	}
	out := new(RollingUpdateCollaSetStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RuleState) DeepCopyInto(out *RuleState) {
	*out = *in
	if in.WebhookStatus != nil {
		in, out := &in.WebhookStatus, &out.WebhookStatus
		*out = new(WebhookStatus)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new RuleState.
func (in *RuleState) DeepCopy() *RuleState {
	if in == nil {
		return nil
	}
	out := new(RuleState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ScaleStrategy) DeepCopyInto(out *ScaleStrategy) {
	*out = *in
	if in.PodToExclude != nil {
		in, out := &in.PodToExclude, &out.PodToExclude
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PodToInclude != nil {
		in, out := &in.PodToInclude, &out.PodToInclude
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PersistentVolumeClaimRetentionPolicy != nil {
		in, out := &in.PersistentVolumeClaimRetentionPolicy, &out.PersistentVolumeClaimRetentionPolicy
		*out = new(PersistentVolumeClaimRetentionPolicy)
		**out = **in
	}
	if in.OperationDelaySeconds != nil {
		in, out := &in.OperationDelaySeconds, &out.OperationDelaySeconds
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ScaleStrategy.
func (in *ScaleStrategy) DeepCopy() *ScaleStrategy {
	if in == nil {
		return nil
	}
	out := new(ScaleStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TaskInfo) DeepCopyInto(out *TaskInfo) {
	*out = *in
	if in.Processing != nil {
		in, out := &in.Processing, &out.Processing
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Approved != nil {
		in, out := &in.Approved, &out.Approved
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.BeginTime != nil {
		in, out := &in.BeginTime, &out.BeginTime
		*out = (*in).DeepCopy()
	}
	if in.LastTime != nil {
		in, out := &in.LastTime, &out.LastTime
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TaskInfo.
func (in *TaskInfo) DeepCopy() *TaskInfo {
	if in == nil {
		return nil
	}
	out := new(TaskInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TransitionRule) DeepCopyInto(out *TransitionRule) {
	*out = *in
	if in.Stage != nil {
		in, out := &in.Stage, &out.Stage
		*out = new(string)
		**out = **in
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Filter != nil {
		in, out := &in.Filter, &out.Filter
		*out = new(TransitionRuleFilter)
		(*in).DeepCopyInto(*out)
	}
	in.TransitionRuleDefinition.DeepCopyInto(&out.TransitionRuleDefinition)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TransitionRule.
func (in *TransitionRule) DeepCopy() *TransitionRule {
	if in == nil {
		return nil
	}
	out := new(TransitionRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TransitionRuleDefinition) DeepCopyInto(out *TransitionRuleDefinition) {
	*out = *in
	if in.AvailablePolicy != nil {
		in, out := &in.AvailablePolicy, &out.AvailablePolicy
		*out = new(AvailableRule)
		(*in).DeepCopyInto(*out)
	}
	if in.LabelCheck != nil {
		in, out := &in.LabelCheck, &out.LabelCheck
		*out = new(LabelCheckRule)
		(*in).DeepCopyInto(*out)
	}
	if in.Webhook != nil {
		in, out := &in.Webhook, &out.Webhook
		*out = new(TransitionRuleWebhook)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TransitionRuleDefinition.
func (in *TransitionRuleDefinition) DeepCopy() *TransitionRuleDefinition {
	if in == nil {
		return nil
	}
	out := new(TransitionRuleDefinition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TransitionRuleFilter) DeepCopyInto(out *TransitionRuleFilter) {
	*out = *in
	if in.LabelSelector != nil {
		in, out := &in.LabelSelector, &out.LabelSelector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TransitionRuleFilter.
func (in *TransitionRuleFilter) DeepCopy() *TransitionRuleFilter {
	if in == nil {
		return nil
	}
	out := new(TransitionRuleFilter)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TransitionRuleWebhook) DeepCopyInto(out *TransitionRuleWebhook) {
	*out = *in
	in.ClientConfig.DeepCopyInto(&out.ClientConfig)
	if in.FailurePolicy != nil {
		in, out := &in.FailurePolicy, &out.FailurePolicy
		*out = new(FailurePolicyType)
		**out = **in
	}
	if in.Parameters != nil {
		in, out := &in.Parameters, &out.Parameters
		*out = make([]Parameter, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TransitionRuleWebhook.
func (in *TransitionRuleWebhook) DeepCopy() *TransitionRuleWebhook {
	if in == nil {
		return nil
	}
	out := new(TransitionRuleWebhook)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *UpdateStrategy) DeepCopyInto(out *UpdateStrategy) {
	*out = *in
	if in.RollingUpdate != nil {
		in, out := &in.RollingUpdate, &out.RollingUpdate
		*out = new(RollingUpdateCollaSetStrategy)
		(*in).DeepCopyInto(*out)
	}
	if in.OperationDelaySeconds != nil {
		in, out := &in.OperationDelaySeconds, &out.OperationDelaySeconds
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new UpdateStrategy.
func (in *UpdateStrategy) DeepCopy() *UpdateStrategy {
	if in == nil {
		return nil
	}
	out := new(UpdateStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WebhookRequest) DeepCopyInto(out *WebhookRequest) {
	*out = *in
	if in.Stage != nil {
		in, out := &in.Stage, &out.Stage
		*out = new(string)
		**out = **in
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = make([]ResourceParameter, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WebhookRequest.
func (in *WebhookRequest) DeepCopy() *WebhookRequest {
	if in == nil {
		return nil
	}
	out := new(WebhookRequest)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WebhookResponse) DeepCopyInto(out *WebhookResponse) {
	*out = *in
	if in.FinishedNames != nil {
		in, out := &in.FinishedNames, &out.FinishedNames
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WebhookResponse.
func (in *WebhookResponse) DeepCopy() *WebhookResponse {
	if in == nil {
		return nil
	}
	out := new(WebhookResponse)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WebhookStatus) DeepCopyInto(out *WebhookStatus) {
	*out = *in
	if in.TaskStates != nil {
		in, out := &in.TaskStates, &out.TaskStates
		*out = make([]TaskInfo, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.History != nil {
		in, out := &in.History, &out.History
		*out = make([]TaskInfo, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new WebhookStatus.
func (in *WebhookStatus) DeepCopy() *WebhookStatus {
	if in == nil {
		return nil
	}
	out := new(WebhookStatus)
	in.DeepCopyInto(out)
	return out
}
