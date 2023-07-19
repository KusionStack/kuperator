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
	// --- Begin: Labels for Pod Lifecycle ---

	PodPrepareUpgradePhaseLabel = "lifecycle.cafe.kusionstack.io/prepare-upgrade"
	PodUpgradePhaseLabel        = "lifecycle.cafe.kusionstack.io/upgrade" // indicate a pod is in upgrade lifecycle. It will be removed when pod is upgraded and ready
	PodFinishUpgradePhaseLabel  = "lifecycle.cafe.kusionstack.io/finish-upgrade"
	PodTerminatePhaseLabel      = "cafe.kusionstack.io/terminating"       // indicate a pod is in terminating
	PodPreCheckLabel            = "cafe.kusionstack.io/pre-check"         // need check before start upgrading pods
	PodPostCheckLabel           = "cafe.kusionstack.io/post-check"        // need check after stop upgrading pods
	PodJustCreated              = "cafe.kusionstack.io/just-created"      // mark this resource is just created
	PodLabelServiceAvailable    = "cafe.kusionstack.io/service-available" // indicate a pod's service is available

	// PodDuringUpgradeLabel indicate a pod is inPlace upgrading. If inPlace update finished, the label will be removed. The Pod could not be ready then.
	// It is recommended in format: <mark-operator>-<timestamp in int64>
	PodDuringUpgradeLabel = "cafe.kusionstack.io/upgrading"

	// PodLabelUpgradeByRecreate indicates a pod will be upgraded by recreating instead of in-place upgrade
	PodLabelUpgradeByRecreate = "pre-check.condition.cafe.kusionstack.io/upgrade-by-recreating"

	// --- End: Labels for Pod Lifecycle ---
)

const (
	LabelPodOperating           = "operating.kafed.kusionstack.io"
	LabelPodOperationType       = "operation-type.kafed.kusionstack.io"
	LabelPodOperationPermission = "operation-permission.kafed.kusionstack.io"
	LabelPodUndoOperationType   = "undo-operation-type.kafed.kusionstack.io"
	LabelPodDoneOperationType   = "done-operation-type.kafed.kusionstack.io"

	LabelPodPreCheck    = "pre-check.lifecycle.kafed.kusionstack.io"
	LabelPodPreChecked  = "pre-checked.lifecycle.kafed.kusionstack.io"
	LabelPodPrepare     = "prepare.lifecycle.kafed.kusionstack.io"
	LabelPodOperate     = "operate.lifecycle.kafed.kusionstack.io"
	LabelPodOperated    = "operated.lifecycle.kafed.kusionstack.io"
	LabelPodPostCheck   = "post-check.lifecycle.kafed.kusionstack.io"
	LabelPodPostChecked = "post-checked.lifecycle.kafed.kusionstack.io"
	LabelPodComplete    = "complete.lifecycle.kafed.kusionstack.io"
)

const (
	PodTerminatingPhaseLabel = "cafed.kusionstack.io/terminating" // indicate a pod is in terminating
	PodScalingInPhaseLabel   = "cafed.kusionstack.io/scaling-in"  // indicate a pod is scaling in
	PodUpgradingPhaseLabel   = "cafed.kusionstack.io/upgrading"   // indicate a pod is upgrading
	PodReCreatingPhaseLabel  = "cafed.kusionstack.io/recreating"  // indicate a pod is recreating
)
