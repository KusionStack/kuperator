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

// pod ops lifecyle labels
const (
	ControlledByKusionStackLabelKey = "kusionstack.io/control"

	PodOperatingLabelPrefix           = "operating.podopslifecycle.kusionstack.io"            // indicate a pod is operating
	PodOperationTypeLabelPrefix       = "operation-type.podopslifecycle.kusionstack.io"       // indicate the type of operation
	PodOperationPermissionLabelPrefix = "operation-permission.podopslifecycle.kusionstack.io" // indicate the permission of operation
	PodUndoOperationTypeLabelPrefix   = "undo-operation-type.podopslifecycle.kusionstack.io"  // indicate the type of operation has been canceled
	PodDoneOperationTypeLabelPrefix   = "done-operation-type.podopslifecycle.kusionstack.io"  // indicate the type of operation has been done

	PodPreCheckLabelPrefix    = "pre-check.podopslifecycle.kusionstack.io"    // indicate a pod is in pre-check phase
	PodPreCheckedLabelPrefix  = "pre-checked.podopslifecycle.kusionstack.io"  // indicate a pod has finished pre-check phase
	PodPreparingLabelPrefix   = "preparing.podopslifecycle.kusionstack.io"    // indicate a pod is preparing for operation
	PodOperateLabelPrefix     = "operate.podopslifecycle.kusionstack.io"      // indicate a pod is in operate phase
	PodOperatedLabelPrefix    = "operated.podopslifecycle.kusionstack.io"     // indicate a pod has finished operate phase
	PodPostCheckLabelPrefix   = "post-check.podopslifecycle.kusionstack.io"   // indicate a pod is in post-check phase
	PodPostCheckedLabelPrefix = "post-checked.podopslifecycle.kusionstack.io" // indicate a pod has finished post-check phase
	PodCompletingLabelPrefix  = "completing.podopslifecycle.kusionstack.io"   // indicate a pod is completing operation

	PodServiceAvailableLabel          = "podopslifecycle.kusionstack.io/service-available" // indicate a pod is available to serve
	PodDeletionIndicationLabelKey     = "podopslifecycle.kusionstack.io/to-delete"         // users can use this label to indicate a pod to delete
	PodReplaceIndicationLabelKey      = "podopslifecycle.kusionstack.io/to-replace"        // users can use this label to indicate a pod to replace
	PodReplaceByReplaceUpdateLabelKey = "podopslifecycle.kusionstack.io/replaced-by-replace-update"

	PodInstanceIDLabelKey = "collaset.kusionstack.io/instance-id" // used to attach Pod instance ID on Pod

	PodReplacePairOriginName = "collaset.kusionstack.io/replace-pair-origin-name" // used to indicate the original Pod name for replacement.

	PodReplacePairNewId = "collaset.kusionstack.io/replace-pair-new-id" // used to indicate the new created Pod instance ID for replacement.
)

const (
	CollaSetUpdateIndicateLabelKey = "collaset.kusionstack.io/update-included"
)

var (
	WellKnownLabelPrefixesWithID = []string{PodOperatingLabelPrefix, PodOperationTypeLabelPrefix, PodPreCheckLabelPrefix, PodPreCheckedLabelPrefix,
		PodPreparingLabelPrefix, PodDoneOperationTypeLabelPrefix, PodUndoOperationTypeLabelPrefix, PodOperateLabelPrefix, PodOperatedLabelPrefix, PodPostCheckLabelPrefix,
		PodPostCheckedLabelPrefix, PodCompletingLabelPrefix}
)
