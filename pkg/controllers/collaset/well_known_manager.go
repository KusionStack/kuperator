/*
Copyright 2025 The KusionStack Authors.

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

package collaset

import (
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/kube-xset/api"
)

var defaultXSetControllerLabelManager = map[api.XSetLabelAnnotationEnum]string{
	api.OperatingLabelPrefix:         appsv1alpha1.PodOperatingLabelPrefix,
	api.OperationTypeLabelPrefix:     appsv1alpha1.PodOperationTypeLabelPrefix,
	api.OperateLabelPrefix:           appsv1alpha1.PodOperateLabelPrefix,
	api.UndoOperationTypeLabelPrefix: appsv1alpha1.PodUndoOperationTypeLabelPrefix,
	api.ServiceAvailableLabel:        appsv1alpha1.PodServiceAvailableLabel,
	api.PreparingDeleteLabel:         appsv1alpha1.PodPreparingDeleteLabel,

	api.ControlledByXSetLabel:           appsv1alpha1.ControlledByKusionStackLabelKey,
	api.XInstanceIdLabelKey:             appsv1alpha1.PodInstanceIDLabelKey,
	api.XSetUpdateIndicationLabelKey:    appsv1alpha1.CollaSetUpdateIndicateLabelKey,
	api.XDeletionIndicationLabelKey:     appsv1alpha1.PodDeletionIndicationLabelKey,
	api.XReplaceIndicationLabelKey:      appsv1alpha1.PodReplaceIndicationLabelKey,
	api.XReplacePairNewId:               appsv1alpha1.PodReplacePairNewId,
	api.XReplacePairOriginName:          appsv1alpha1.PodReplacePairOriginName,
	api.XReplaceByReplaceUpdateLabelKey: appsv1alpha1.PodReplaceByReplaceUpdateLabelKey,
	api.XOrphanedIndicationLabelKey:     appsv1alpha1.PodOrphanedIndicateLabelKey,
	api.XCreatingLabel:                  appsv1alpha1.PodCreatingLabel,
	api.XCompletingLabel:                appsv1alpha1.PodCompletingLabel,
	api.XExcludeIndicationLabelKey:      appsv1alpha1.PodExcludeIndicationLabelKey,

	api.SubResourcePvcTemplateLabelKey:     appsv1alpha1.PvcTemplateLabelKey,
	api.SubResourcePvcTemplateHashLabelKey: appsv1alpha1.PvcTemplateHashLabelKey,
}
