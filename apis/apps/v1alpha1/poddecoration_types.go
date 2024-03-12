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

type MetadataPatchPolicy string

const (
	RetainMetadata         MetadataPatchPolicy = "Retain"
	OverwriteMetadata      MetadataPatchPolicy = "Overwrite"
	MergePatchJsonMetadata MetadataPatchPolicy = "MergePatchJson"
)

type ContainerInjectPolicy string

const (
	BeforePrimaryContainer ContainerInjectPolicy = "BeforePrimaryContainer"
	AfterPrimaryContainer  ContainerInjectPolicy = "AfterPrimaryContainer"
)

type PrimaryContainerInjectTargetPolicy string

const (
	InjectByName         PrimaryContainerInjectTargetPolicy = "ByName"
	InjectAllContainers  PrimaryContainerInjectTargetPolicy = "All"
	InjectFirstContainer PrimaryContainerInjectTargetPolicy = "First"
	InjectLastContainer  PrimaryContainerInjectTargetPolicy = "Last"
)

type PodDecorationPodTemplate struct {
	// Metadata is the ResourceDecoration to attach on pod metadata
	Metadata []*PodDecorationPodTemplateMeta `json:"metadata,omitempty"`

	// InitContainers is the init containers needs to be attached to a pod.
	// If there is a container with the same name, PodDecoration will retain old Container.
	InitContainers []*corev1.Container `json:"initContainers,omitempty"`

	// Containers is the containers need to be attached to a pod.
	// If there is a container with the same name, PodDecoration will override it entirely.
	Containers []*ContainerPatch `json:"containers,omitempty"`

	// PrimaryContainers contains the configuration to merge into the primary container.
	// Name in it is not required. If a name indicated, then merge to the container with the matched name,
	// otherwise merge to the one indicated by its policy.
	PrimaryContainers []*PrimaryContainerPatch `json:"primaryContainers,omitempty"`

	// Volumes will be attached to a pod spec volume.
	// If there is a volume with the same name, new volume will replace it.
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// If specified, the pod's scheduling constraints
	// +optional
	Affinity *PodDecorationAffinity `json:"affinity,omitempty"`

	// If specified, the pod's tolerations.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// RuntimeClassName refers to a RuntimeClass object in the node.k8s.io group, which should be used
	// to run this pod.  If no RuntimeClass resource matches the named class, the pod will not be run.
	// If unset or empty, the "legacy" RuntimeClass will be used, which is an implicit class with an
	// empty definition that uses the default runtime handler.
	// More info: https://git.k8s.io/enhancements/keps/sig-node/runtime-class.md
	// This is a beta feature as of Kubernetes v1.14.
	// +optional
	RuntimeClassName *string `json:"runtimeClassName,omitempty"`
}

type PodDecorationPodTemplateMeta struct {

	// patch pod metadata policy, Default is "Retain"
	PatchPolicy MetadataPatchPolicy `json:"patchPolicy"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

type ContainerPatch struct {
	// InjectPolicy indicates the position to inject the Container configuration.
	// Default is BeforePrimaryContainer.
	// +optional
	InjectPolicy ContainerInjectPolicy `json:"injectPolicy"`

	corev1.Container `json:",inline"`
}

type PrimaryContainerPatch struct {
	// TargetPolicy indicates which app container these configuration should inject into.
	// Default is LastAppContainerTargetSelectPolicy
	TargetPolicy PrimaryContainerInjectTargetPolicy `json:"targetPolicy,omitempty"`

	PodDecorationPrimaryContainer `json:",inline"`
}

// PodDecorationPrimaryContainer contains the decoration configuration to override the application container.
type PodDecorationPrimaryContainer struct {
	// Name indicates target container name
	Name *string `json:"name,omitempty"`

	// Image indicates a new image to override the one in application container.
	Image *string `json:"image,omitempty"`

	// AppEnvs is the env variables that will be injected into application container.
	Env []corev1.EnvVar `json:"env,omitempty"`

	// VolumeMounts indicates the volume mount list which is injected into app container volume mount list.
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// PodDecorationAffinity carries the configuration to inject into the Pod affinity.
type PodDecorationAffinity struct {
	// OverrideAffinity indicates the pod's scheduling constraints. It is applied by overriding.
	// +optional
	OverrideAffinity *corev1.Affinity `json:"overrideAffinity,omitempty"`

	// NodeSelectorTerms indicates the node selector to append into the existing requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms.
	NodeSelectorTerms []corev1.NodeSelectorTerm `json:"nodeSelectorTerms,omitempty"`
}

type PodDecorationUpdateStrategy struct {
	// RollingUpdate provides several ways to select Pods to update to target revision.
	RollingUpdate *PodDecorationRollingUpdate `json:"rollingUpdate,omitempty"`
}

type PodDecorationRollingUpdate struct {
	// Partition controls the update progress by indicating how many pods should be updated.
	// Partition value indicates the number of Pods which should be updated to the updated revision.
	// Defaults to nil (all pods will be updated)
	// +optional
	Partition *int32 `json:"partition,omitempty"`

	// Selector indicates the update progress is controlled by selector.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// PodDecorationSpec defines the desired state of PodDecoration
type PodDecorationSpec struct {
	// Indicate the number of histories to be conserved
	// If unspecified, defaults to 20
	// +optional
	HistoryLimit int32 `json:"historyLimit,omitempty"`

	// DisablePodDetail used to disable show status pod details
	DisablePodDetail bool `json:"disablePodDetail,omitempty"`

	// Selector is a label query over pods that should be injected with PodDecoration
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// UpdateStrategy carries the strategy configuration for update.
	UpdateStrategy PodDecorationUpdateStrategy `json:"updateStrategy,omitempty"`

	// Weight indicates the priority to apply for a group of PodDecorations with same group value.
	// The greater one has higher priority to apply.
	// Default value is 0.
	Weight *int32 `json:"weight,omitempty"`

	// Template includes the decoration message about pod template.
	Template PodDecorationPodTemplate `json:"template,omitempty"`
}

// PodDecorationStatus defines the observed state of PodDecoration
type PodDecorationStatus struct {
	// ObservedGeneration is the most recent generation observed for this PodDecoration. It corresponds to the
	// PodDecoration's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// CurrentRevision, if not empty, indicates the version of the PodDecoration.
	// +optional
	CurrentRevision string `json:"currentRevision,omitempty"`

	// UpdatedRevision, if not empty, indicates the version of the PodDecoration currently updated.
	// +optional
	UpdatedRevision string `json:"updatedRevision,omitempty"`

	// Count of hash collisions for the PodDecoration. The PodDecoration controller
	// uses this field as a collision avoidance mechanism when it needs to
	// create the name for the newest ControllerRevision.
	// +optional
	CollisionCount int32 `json:"collisionCount,omitempty"`

	// MatchedPods is the number of Pods whose labels are matched with this PodDecoration's selector
	MatchedPods int32 `json:"matchedPods,omitempty"`

	// UpdatedPods is the number of matched Pods that are injected with the latest PodDecoration's containers
	UpdatedPods int32 `json:"updatedPods,omitempty"`

	// InjectedPods is the number of injected Pods that are injected with this PodDecoration
	InjectedPods int32 `json:"injectedPods,omitempty"`

	// UpdatedReadyPods is the number of matched pods that updated and ready
	UpdatedReadyPods int32 `json:"updatedReadyPods,omitempty"`

	// UpdatedAvailablePods indicates the number of available updated revision replicas for this PodDecoration.
	// A pod is updated available means the pod is ready for updated revision and accessible
	// +optional
	UpdatedAvailablePods int32 `json:"updatedAvailablePods,omitempty"`

	// IsEffective indicates PodDecoration is the only one that takes effect in the same group
	IsEffective *bool `json:"isEffective,omitempty"`

	// Details record the update information of CollaSets and Pods
	Details []PodDecorationWorkloadDetail `json:"details,omitempty"`
}

type PodDecorationWorkloadDetail struct {
	CollaSet         string                 `json:"collaSet,omitempty"`
	AffectedReplicas int32                  `json:"affectedReplicas,omitempty"`
	Pods             []PodDecorationPodInfo `json:"pods,omitempty"`
}

type PodDecorationPodInfo struct {
	Name     string `json:"name,omitempty"`
	Revision string `json:"revision,omitempty"`
	Escaped  bool   `json:"escaped,omitempty"`
}

type PodDecorationCondition struct {
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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodDecoration is the Schema for the poddecorations API
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=pd
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="EFFECTIVE",type="boolean",JSONPath=".status.isEffective",description="The number of pods updated."
// +kubebuilder:printcolumn:name="MATCHED",type="integer",JSONPath=".status.matchedPods",description="The number of selected pods."
// +kubebuilder:printcolumn:name="INJECTED",type="integer",JSONPath=".status.injectedPods",description="The number of injected pods."
// +kubebuilder:printcolumn:name="UPDATED",type="integer",JSONPath=".status.updatedPods",description="The number of updated pods."
// +kubebuilder:printcolumn:name="UPDATED_READY",type="integer",JSONPath=".status.updatedReadyPods",description="The number of pods ready."
// +kubebuilder:printcolumn:name="CURRENT_REVISION",type="string",JSONPath=".status.currentRevision",description="The current revision."
// +kubebuilder:printcolumn:name="UPDATED_REVISION",type="string",JSONPath=".status.updatedRevision",description="The updated revision."
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +resource:path=poddecorations
type PodDecoration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodDecorationSpec   `json:"spec,omitempty"`
	Status PodDecorationStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodDecorationList contains a list of PodDecoration
type PodDecorationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodDecoration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodDecoration{}, &PodDecorationList{})
}
