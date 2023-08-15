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
)

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
func (in *UpdateStrategy) DeepCopyInto(out *UpdateStrategy) {
	*out = *in
	if in.RollingUpdate != nil {
		in, out := &in.RollingUpdate, &out.RollingUpdate
		*out = new(RollingUpdateCollaSetStrategy)
		(*in).DeepCopyInto(*out)
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
