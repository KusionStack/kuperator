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
	PodAvailableConditionsAnnotation = "pod.kusionstack.io/available-conditions" // indicate the available conditions of a pod

	LastPodStatusAnnotationKey = "collaset.kusionstack.io/last-pod-status"
)

// PodTransitionRule Annotation
const (
	AnnotationPodSkipRuleConditions         = "podtransitionrule.kusionstack.io/skip-rule-conditions"
	AnnotationPodTransitionRuleDetailPrefix = "detail.podtransitionrule.kusionstack.io"
)

// PodDecoration Annotation
const (
	// AnnotationPodDecorationRevision struct: { groupName: {name: pdName, revision: currentRevision}, groupName: {} }
	AnnotationPodDecorationRevision = "poddecoration.kusionstack.io/revisions"
)

// GraceDelete Webhook Annotation
const (
	// AnnotationPodDecorationRevision struct: { groupName: {name: pdName, revision: currentRevision}, groupName: {} }
	AnnotationGraceDeleteTimestamp = "gracedelete.kusionstack.io/delete-timestamp"
)

// OperationJob Annotation
const (
	// AnnotationRecreateMethod string, e.g., ContainerRecreateRequest
	AnnotationOperationJobRecreateMethod = "operationjob.kusionstack.io/recreate-method"
)
