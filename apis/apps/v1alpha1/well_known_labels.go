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

const (
	PodScalingInPhaseLabel = "cafed.kusionstack.io/scaling-in" // indicate a pod is scaling in

	PodOperatingLabelPrefix           = "operating.kafed.kusionstack.io"            // indicate a pod is operating
	PodOperationTypeLabelPrefix       = "operation-type.kafed.kusionstack.io"       // indicate the type of operation
	PodOperationPermissionLabelPrefix = "operation-permission.kafed.kusionstack.io" // indicate the permission of operation
	PodUndoOperationTypeLabelPrefix   = "undo-operation-type.kafed.kusionstack.io"  // indicate the type of operation has been canceled
	PodDoneOperationTypeLabelPrefix   = "done-operation-type.kafed.kusionstack.io"  // indicate the type of operation has been done

	PodPreCheckLabelPrefix    = "pre-check.lifecycle.kafed.kusionstack.io"    // indicate a pod is in pre-check phase
	PodPreCheckedLabelPrefix  = "pre-checked.lifecycle.kafed.kusionstack.io"  // indicate a pod has finished pre-check phase
	PodPrepareLabelPrefix     = "prepare.lifecycle.kafed.kusionstack.io"      // indicate a pod is in prepare phase
	PodOperateLabelPrefix     = "operate.lifecycle.kafed.kusionstack.io"      // indicate a pod is in operate phase
	PodOperatedLabelPrefix    = "operated.lifecycle.kafed.kusionstack.io"     // indicate a pod has finished operate phase
	PodPostCheckLabelPrefix   = "post-check.lifecycle.kafed.kusionstack.io"   // indicate a pod is in post-check phase
	PodPostCheckedLabelPrefix = "post-checked.lifecycle.kafed.kusionstack.io" // indicate a pod has finished post-check phase
	PodCompleteLabelPrefix    = "complete.lifecycle.kafed.kusionstack.io"     // indicate a pod has finished all phases

	PodServiceAvailableLabel = "kafed.kusionstack.io/service-available" // indicate a pod is available to serve
	// --- Begin: Labels for CollaSet ---

	CollaSetUpdateIndicateLabelKey = "apps.kafed.kusionstack.io/update-included"
	PodInstanceIDLabelKey          = "kafed.kusionstack.io/pod-instance-id"
	PodScalingInLabelKey           = "apps.kafed.kusionstack.io/scaling-in"

	// --- End: Labels for CollaSet ---

	PodDeletionIndicationLabelKey = "kafed.kusionstack.io/to-delete" // Users can use this label to indicate a pod to delete
)

var (
	WellKnownLabelPrefixesWithID = []string{PodOperatingLabelPrefix, PodOperationTypeLabelPrefix, PodPreCheckLabelPrefix, PodPreCheckedLabelPrefix,
		PodPrepareLabelPrefix, PodUndoOperationTypeLabelPrefix, PodOperateLabelPrefix, PodOperatedLabelPrefix, PodPostCheckLabelPrefix,
		PodPostCheckedLabelPrefix, PodCompleteLabelPrefix}
)
