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
	OpsActionRecreate OpsAction = "Recreate"
	OpsActionReplace  OpsAction = "Replace"
)

// ReasonForOpsProgress defines the reason of pod operation progress
type ReasonForOpsProgress string

const (
	ReasonPodNotFound       ReasonForOpsProgress = "PodNotFound"
	ReasonContainerNotFound ReasonForOpsProgress = "ContainerNotFound"
)

// OperationProgress indicates operation progress of pod
type OperationProgress string

const (
	OperationProgressPending    OperationProgress = "Pending"
	OperationProgressProcessing OperationProgress = "Processing"
	OperationProgressFailed     OperationProgress = "Failed"
	OperationProgressSucceeded  OperationProgress = "Succeeded"
)

// OperationJobSpec defines the desired state of OperationJob
type OperationJobSpec struct {
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

// PodOpsTarget defines the target pods of the OperationJob
type PodOpsTarget struct {
	// Specify the operation target pods
	// +optional
	PodName string `json:"podName,omitempty"`

	// Specify the containers to restart
	// +optional
	Containers []string `json:"containers,omitempty"`
}

// OperationJobStatus defines the observed state of OperationJob
type OperationJobStatus struct {
	// Phase indicates the of the OperationJob
	// +optional
	Progress OperationProgress `json:"progress,omitempty"`

	// Operation start time
	// +optional
	StartTimestamp *metav1.Time `json:"startTimestamp,omitempty"`

	// Operation end time
	// +optional
	EndTimestamp *metav1.Time `json:"endTimestamp,omitempty"`

	// Replicas of the pods involved in the OperationJob
	// +optional
	TotalPodCount int32 `json:"totalPodCount,omitempty"`

	// Succeeded replicas of the pods involved in the OperationJob
	// +optional
	SucceededPodCount int32 `json:"succeededPodCount,omitempty"`

	// failed pod count of the pods involved in the OperationJob
	// +optional
	FailedPodCount int32 `json:"failedPodCount,omitempty"`

	// Operation details of the target pods
	// +optional
	PodDetails []PodOpsStatus `json:"targetDetails,omitempty"`
}

type PodOpsStatus struct {
	// name of the target pod
	// +optional
	PodName string `json:"podName,omitempty"`

	// operation progress of target pod
	// +optional
	Progress OperationProgress `json:"progress,omitempty"`

	// reason for current operation progress
	// +optional
	Reason ReasonForOpsProgress `json:"reason,omitempty"`

	// message displays detail of reason
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:shortName=oj
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="PROGRESS",type="string",JSONPath=".status.progress"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// OperationJob is the Schema for the operationjobs API
type OperationJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OperationJobSpec   `json:"spec,omitempty"`
	Status OperationJobStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OperationJobList contains a list of OperationJob
type OperationJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OperationJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OperationJob{}, &OperationJobList{})
}
