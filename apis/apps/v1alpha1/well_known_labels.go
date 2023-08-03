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
