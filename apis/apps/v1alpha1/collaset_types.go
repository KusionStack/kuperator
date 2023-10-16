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
)

type CollaSetConditionType string

const (
	CollaSetScale  CollaSetConditionType = "Scale"
	CollaSetUpdate CollaSetConditionType = "Update"
)

// PersistentVolumeClaimRetentionPolicyType is a string enumeration of the policies that will determine
// which action will be applied on volumes from the VolumeClaimTemplates when the CollaSet is
// deleted or scaled down.
type PersistentVolumeClaimRetentionPolicyType string

const (
	// RetainPersistentVolumeClaimRetentionPolicyType specifies that
	// PersistentVolumeClaims associated with CollaSet VolumeClaimTemplates
	// will not be deleted.
	RetainPersistentVolumeClaimRetentionPolicyType PersistentVolumeClaimRetentionPolicyType = "Retain"
	// DeletePersistentVolumeClaimRetentionPolicyType is the default policy, which specifies that
	// PersistentVolumeClaims associated with CollaSet VolumeClaimTemplates
	// will be deleted in the scenario specified in PersistentVolumeClaimRetentionPolicy.
	DeletePersistentVolumeClaimRetentionPolicyType PersistentVolumeClaimRetentionPolicyType = "Delete"
)

// PodUpdateStrategyType is a string enumeration type that enumerates
// all possible ways we can update a Pod when updating application
type PodUpdateStrategyType string

const (
	// CollaSetRecreatePodUpdateStrategyType indicates that CollaSet will always update Pod by deleting and recreate it.
	CollaSetRecreatePodUpdateStrategyType PodUpdateStrategyType = "ReCreate"
	// CollaSetInPlaceIfPossiblePodUpdateStrategyType indicates thath CollaSet will try to update Pod by in-place update
	// when it is possible. Recently, only Pod image can be updated in-place. Any other Pod spec change will make the
	// policy fall back to CollaSetRecreatePodUpdateStrategyType.
	CollaSetInPlaceIfPossiblePodUpdateStrategyType PodUpdateStrategyType = "InPlaceIfPossible"
	// CollaSetInPlaceOnlyPodUpdateStrategyType indicates that CollaSet will always update Pod in-place, instead of
	// recreating pod. It will encounter an error on original Kubernetes cluster.
	CollaSetInPlaceOnlyPodUpdateStrategyType PodUpdateStrategyType = "InPlaceOnly"
)

// CollaSetSpec defines the desired state of CollaSet
type CollaSetSpec struct {
	// Indicates that the scaling and updating is paused and will not be processed by the
	// CollaSet controller.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// Replicas is the desired number of replicas of the given Template.
	// These are replicas in the sense that they are instantiations of the
	// same Template, but individual replicas also have a consistent identity.
	// If unspecified, defaults to 0.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Selector is a label query over pods that should match the replica count.
	// It must match the pod template's labels.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Template is the object that describes the pod that will be created if
	// insufficient replicas are detected. Each pod stamped out by the CollaSet
	// will fulfill this Template, but have a unique identity from the rest
	// of the CollaSet.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	Template corev1.PodTemplateSpec `json:"template,omitempty"`

	// VolumeClaimTemplates is a list of claims that pods are allowed to reference.
	// The StatefulSet controller is responsible for mapping network identities to
	// claims in a way that maintains the identity of a pod. Every claim in
	// this list must have at least one matching (by name) volumeMount in one
	// container in the template. A claim in this list takes precedence over
	// any volumes in the template, with the same name.
	// +optional
	VolumeClaimTemplates []corev1.PersistentVolumeClaim `json:"volumeClaimTemplates,omitempty"`

	// UpdateStrategy indicates the CollaSetUpdateStrategy that will be
	// employed to update Pods in the CollaSet when a revision is made to
	// Template.
	// +optional
	UpdateStrategy UpdateStrategy `json:"updateStrategy,omitempty"`

	// ScaleStrategy indicates the strategy detail that will be used during pod scaling.
	// +optional
	ScaleStrategy ScaleStrategy `json:"scaleStrategy,omitempty"`

	// Indicate the number of histories to be conserved
	// If unspecified, defaults to 20
	// +optional
	HistoryLimit int32 `json:"historyLimit,omitempty"`
}

type ScaleStrategy struct {
	// Context indicates the pool from which to allocate Pod instance ID. CollaSets are allowed to share the
	// same Context. It is not allowed to change.
	// Context defaults to be CollaSet's name.
	// +optional
	Context string `json:"context,omitempty"`

	// PodToExclude indicates the pods which will be orphaned by CollaSet.
	// +optional
	PodToExclude []string `json:"podToExclude,omitempty"`

	// PodToInclude indicates the pods which will be adapted by CollaSet.
	// +optional
	PodToInclude []string `json:"podToInclude,omitempty"`

	// PersistentVolumeClaimRetentionPolicy describes the lifecycle of PersistentVolumeClaim
	// created from volumeClaimTemplates. By default, all persistent volume claims are created as needed and
	// deleted after no pod is using them. This policy allows the lifecycle to be altered, for example
	// by deleting persistent volume claims when their CollaSet is deleted, or when their pod is scaled down.
	// +optional
	PersistentVolumeClaimRetentionPolicy *PersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// OperationDelaySeconds indicates how many seconds it should delay before operating scale.
	// +optional
	OperationDelaySeconds *int32 `json:"operationDelaySeconds,omitempty"`
}

type PersistentVolumeClaimRetentionPolicy struct {
	// WhenDeleted specifies what happens to PVCs created from CollaSet
	// VolumeClaimTemplates when the CollaSet is deleted. The default policy
	// of `Delete` policy causes those PVCs to be deleted.
	//`Retain` causes PVCs to not be affected by StatefulSet deletion. The
	// +optional
	WhenDeleted PersistentVolumeClaimRetentionPolicyType `json:"whenDeleted,omitempty"`

	// WhenScaled specifies what happens to PVCs created from StatefulSet
	// VolumeClaimTemplates when the StatefulSet is scaled down. The default
	// policy of `Retain` causes PVCs to not be affected by a scaledown. The
	// `Delete` policy causes the associated PVCs for any excess pods above
	// the replica count to be deleted.
	// +optional
	WhenScaled PersistentVolumeClaimRetentionPolicyType `json:"whenScaled,omitempty"`
}

type ByPartition struct {
	// Partition controls the update progress by indicating how many pods should be updated.
	// Defaults to nil (all pods will be updated)
	// +optional
	Partition *int32 `json:"partition,omitempty"`
}

type ByLabel struct {
}

// RollingUpdateCollaSetStrategy is used to communicate parameter for rolling update.
type RollingUpdateCollaSetStrategy struct {
	// ByPartition indicates the update progress is controlled by partition value.
	// +optional
	ByPartition *ByPartition `json:"byPartition,omitempty"`

	// ByLabel indicates the update progress is controlled by attaching pod label.
	// +optional
	ByLabel *ByLabel `json:"byLabel,omitempty"`
}

type UpdateStrategy struct {
	// RollingUpdate is used to communicate parameters when Type is RollingUpdateStatefulSetStrategyType.
	// +optional
	RollingUpdate *RollingUpdateCollaSetStrategy `json:"rollingUpdate,omitempty"`

	// PodUpdatePolicy indicates the policy by to update pods.
	// +optional
	PodUpdatePolicy PodUpdateStrategyType `json:"podUpgradePolicy,omitempty"`

	// OperationDelaySeconds indicates how many seconds it should delay before operating update.
	// +optional
	OperationDelaySeconds *int32 `json:"operationDelaySeconds,omitempty"`
}

// CollaSetStatus defines the observed state of CollaSet
type CollaSetStatus struct {
	// ObservedGeneration is the most recent generation observed for this CollaSet. It corresponds to the
	// CollaSet's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// CurrentRevision, if not empty, indicates the version of the CollaSet.
	// +optional
	CurrentRevision string `json:"currentRevision,omitempty"`

	// UpdatedRevision, if not empty, indicates the version of the CollaSet currently updated.
	// +optional
	UpdatedRevision string `json:"updatedRevision,omitempty"`

	// Count of hash collisions for the CollaSet. The CollaSet controller
	// uses this field as a collision avoidance mechanism when it needs to
	// create the name for the newest ControllerRevision.
	// +optional
	CollisionCount *int32 `json:"collisionCount,omitempty"`

	// the number of scheduled replicas for the CollaSet.
	// +optional
	ScheduledReplicas int32 `json:"scheduledReplicas,omitempty"`

	// ReadyReplicas indicates the number of the pod with ready condition
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// The number of available replicas (ready for at least minReadySeconds) for this replica set.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`

	// Replicas is the most recently observed number of replicas.
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// The number of pods in updated version.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`

	// OperatingReplicas indicates the number of pods during pod ops lifecycle and not finish update-phase.
	// +optional
	OperatingReplicas int32 `json:"operatingReplicas,omitempty"`

	// UpdatedReadyReplicas indicates the number of the pod with updated revision and ready condition
	// +optional
	UpdatedReadyReplicas int32 `json:"updatedReadyReplicas,omitempty"`

	// UpdatedAvailableReplicas indicates the number of available updated revision replicas for this CollaSet.
	// A pod is updated available means the pod is ready for updated revision and accessible
	// +optional
	UpdatedAvailableReplicas int32 `json:"updatedAvailableReplicas,omitempty"`

	// Represents the latest available observations of a CollaSet's current state.
	// +optional
	Conditions []CollaSetCondition `json:"conditions,omitempty"`
}

type CollaSetCondition struct {
	// Type of in place set condition.
	Type CollaSetConditionType `json:"type,omitempty"`

	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status,omitempty"`

	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`

	// The reason for the condition's last transition.
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition.
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// CollaSet is the Schema for the collasets API
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=cls
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="DESIRED",type="integer",JSONPath=".spec.replicas",description="The desired number of pods."
// +kubebuilder:printcolumn:name="CURRENT",type="integer",JSONPath=".status.replicas",description="The number of currently all pods."
// +kubebuilder:printcolumn:name="AVAILABLE",type="integer",JSONPath=".status.availableReplicas",description="The number of pods available."
// +kubebuilder:printcolumn:name="UPDATED",type="integer",JSONPath=".status.updatedReplicas",description="The number of pods updated."
// +kubebuilder:printcolumn:name="UPDATED_READY",type="integer",JSONPath=".status.updatedReadyReplicas",description="The number of pods ready."
// +kubebuilder:printcolumn:name="UPDATED_AVAILABLE",type="integer",JSONPath=".status.updatedAvailableReplicas",description="The number of pods updated available."
// +kubebuilder:printcolumn:name="CURRENT_REVISION",type="string",JSONPath=".status.currentRevision",description="The current revision."
// +kubebuilder:printcolumn:name="UPDATED_REVISION",type="string",JSONPath=".status.updatedRevision",description="The updated revision."
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +resource:path=collasets
type CollaSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CollaSetSpec   `json:"spec,omitempty"`
	Status CollaSetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CollaSetList contains a list of CollaSet
type CollaSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CollaSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CollaSet{}, &CollaSetList{})
}
