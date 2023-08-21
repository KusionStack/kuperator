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
	ControlledByPodOpsLifecycle = "podopslifecycle.kusionstack.io/control" // indicate a pod is controlled by podopslifecycle

	PodOperatingLabelPrefix           = "operating.podopslifecycle.kusionstack.io"            // indicate a pod is operating
	PodOperationTypeLabelPrefix       = "operation-type.podopslifecycle.kusionstack.io"       // indicate the type of operation
	PodOperationPermissionLabelPrefix = "operation-permission.podopslifecycle.kusionstack.io" // indicate the permission of operation
	PodUndoOperationTypeLabelPrefix   = "undo-operation-type.podopslifecycle.kusionstack.io"  // indicate the type of operation has been canceled
	PodDoneOperationTypeLabelPrefix   = "done-operation-type.podopslifecycle.kusionstack.io"  // indicate the type of operation has been done

	PodPreCheckLabelPrefix    = "pre-check.podopslifecycle.kusionstack.io"    // indicate a pod is in pre-check phase
	PodPreCheckedLabelPrefix  = "pre-checked.podopslifecycle.kusionstack.io"  // indicate a pod has finished pre-check phase
	PodPrepareLabelPrefix     = "prepare.podopslifecycle.kusionstack.io"      // indicate a pod is in prepare phase
	PodOperateLabelPrefix     = "operate.podopslifecycle.kusionstack.io"      // indicate a pod is in operate phase
	PodOperatedLabelPrefix    = "operated.podopslifecycle.kusionstack.io"     // indicate a pod has finished operate phase
	PodPostCheckLabelPrefix   = "post-check.podopslifecycle.kusionstack.io"   // indicate a pod is in post-check phase
	PodPostCheckedLabelPrefix = "post-checked.podopslifecycle.kusionstack.io" // indicate a pod has finished post-check phase
	PodCompleteLabelPrefix    = "complete.podopslifecycle.kusionstack.io"     // indicate a pod has finished all phases

	PodServiceAvailableLabel      = "podopslifecycle.kusionstack.io/service-available" // indicate a pod is available to serve
	PodDeletionIndicationLabelKey = "podopslifecycle.kusionstack.io/to-delete"         // users can use this label to indicate a pod to delete

	PodInstanceIDLabelKey = "collaset.kusionstack.io/instance-id" // used to attach Pod instance ID on Pod
)

const (
	CollaSetUpdateIndicateLabelKey = "collaset.kusionstack.io/update-included"
)

var (
	WellKnownLabelPrefixesWithID = []string{PodOperatingLabelPrefix, PodOperationTypeLabelPrefix, PodPreCheckLabelPrefix, PodPreCheckedLabelPrefix,
		PodPrepareLabelPrefix, PodDoneOperationTypeLabelPrefix, PodUndoOperationTypeLabelPrefix, PodOperateLabelPrefix, PodOperatedLabelPrefix, PodPostCheckLabelPrefix,
		PodPostCheckedLabelPrefix, PodCompleteLabelPrefix}
)
