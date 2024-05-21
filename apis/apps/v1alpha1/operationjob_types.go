/*
Copyright 2024 The KusionStack Authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OpsAction defines operation type Recreate and Replace
type OpsAction string

const (
	ActionRecreate   OpsAction = "Recreate"
	OpsActionReplace OpsAction = "Replace"
)

// ExtraInfoKey defines the extra info keys in pod ops status
type ExtraInfoKey string

const (
	ReplacePodNameKey ExtraInfoKey = "ReplaceNewPodName"
	CRRKey            ExtraInfoKey = "ContainerRecreateRequest"
	Reason            ExtraInfoKey = "Reason"
)

// PodOperationProgress indicates the progress of podOperation
type PodOperationProgress string

const (
	OperationProgressPending    PodOperationProgress = "Pending"
	OperationProgressProcessing PodOperationProgress = "Processing"
	OperationProgressFailed     PodOperationProgress = "Failed"
	OperationProgressCompleted  PodOperationProgress = "Completed"
)

// PodPhase indicates operation progress of pod
type PodPhase string

const (
	PodPhaseNotStarted PodPhase = "NotStarted"
	PodPhaseStarted    PodPhase = "Started"
	PodPhaseFailed     PodPhase = "Failed"
	PodPhaseCompleted  PodPhase = "Completed"
)

// ContainerPhase indicates recreate progress of container
type ContainerPhase string

const (
	ContainerPhasePending    ContainerPhase = "Pending"
	ContainerPhaseRecreating ContainerPhase = "Recreating"
	ContainerPhaseFailed     ContainerPhase = "Failed"
	ContainerPhaseSucceed    ContainerPhase = "Succeed"
	ContainerPhaseCompleted  ContainerPhase = "Completed"
)

// PodOperationSpec defines the desired state of PodOperation
type PodOperationSpec struct {
	// Specify the operation actions including: Restart, Replace
	// +optional
	Action OpsAction `json:"action,omitempty"`

	// Define the operation target pods
	// +optional
	Targets []PodOpsTarget `json:"targets,omitempty"`

	// Partition controls the operation progress by indicating how many pods should be operated.
	// Defaults to nil (all pods will be updated)
	// +optional
	Partition *int32 `json:"partition,omitempty"`

	// Specify the duration in seconds relative to the startTime
	// that the job may be active before the system tries to terminate it
	// +optional
	ActiveDeadlineSeconds *int32 `json:"activeDeadlineSeconds,omitempty"`

	// Limit the lifetime of an operation that has finished execution (either Complete or Failed)
	// +optional
	TTLSecondsAfterFinished *int32 `json:"TTLSecondsAfterFinished,omitempty"`
}

// PodOpsTarget defines the target pods of the PodOperation
type PodOpsTarget struct {
	// Specify the operation target pods
	// +optional
	PodName string `json:"podName,omitempty"`

	// Specify the containers to restart
	// +optional
	Containers []string `json:"containers,omitempty"`
}

// PodOperationStatus defines the observed state of PodOperation
type PodOperationStatus struct {
	// Phase indicates the of the PodOperation
	// +optional
	Progress PodOperationProgress `json:"progress,omitempty"`

	// Operation start time
	// +optional
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`

	// Operation end time
	// +optional
	EndTimestamp *metav1.Time `json:"endTimestamp,omitempty"`

	// Replicas of the pods involved in the PodOperation
	// +optional
	TotalReplicas int32 `json:"totalReplicas,omitempty"`

	// Completed replicas of the pods involved in the PodOperation
	// +optional
	CompletedReplicas int32 `json:"completedReplicas,omitempty"`

	// Processing replicas of the pods involved in the PodOperation
	// +optional
	ProcessingReplicas int32 `json:"processingReplicas,omitempty"`

	// failed replicas of the pods involved in the PodOperation
	// +optional
	FailedReplicas int32 `json:"failedReplicas,omitempty"`

	// Operation details of the target pods
	// +optional
	PodDetails []PodOpsStatus `json:"targetDetails,omitempty"`
}

type PodOpsStatus struct {
	// Name of the target pod
	// +optional
	PodName string `json:"podName,omitempty"`

	// progress of target pod
	// +optional
	Phase PodPhase `json:"phase,omitempty"`

	// Extra info of the target pod
	// +optional
	ExtraInfo map[ExtraInfoKey]string `json:"extraInfo,omitempty"`

	// the target container to restart
	// +optional
	ContainerDetails []ContainerOpsStatus `json:"containers,omitempty"`
}

type ContainerOpsStatus struct {
	// Name of the container
	// +optional
	ContainerName string `json:"name,omitempty"`

	// Phase indicates recreate progress of container
	// +optional
	Phase ContainerPhase `json:"phase,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=poo
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="PROGRESS",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// PodOperation is the Schema for the podoperations API
type PodOperation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PodOperationSpec   `json:"spec,omitempty"`
	Status PodOperationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PodOperationList contains a list of PodOperation
type PodOperationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PodOperation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PodOperation{}, &PodOperationList{})
}
