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

package replace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/source"

	. "kusionstack.io/operating/pkg/controllers/operationjob/opscore"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
)

const (
	OperationJobReplacePodFinalizer = "finalizer.operationjob.kusionstack.io/replace-protected"
)

var _ ActionHandler = &PodReplaceHandler{}

type PodReplaceHandler struct{}

func (p *PodReplaceHandler) Init(c client.Client, controller controller.Controller, _ *runtime.Scheme, _ cache.Cache) error {
	// Watch for changes to replace new pods
	err := controller.Watch(&source.Kind{Type: &corev1.Pod{}}, &OriginPodHandler{Client: c})
	if err != nil {
		return err
	}

	return nil
}

func (p *PodReplaceHandler) OperateTarget(ctx context.Context, logger logr.Logger, recorder record.EventRecorder, c client.Client, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) error {
	if candidate.Pod == nil {
		return nil
	}

	// parse replace information from origin pod
	_, replaceIndicated := candidate.Pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
	_, replaceByReplaceUpdate := candidate.Pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
	_, replaceNewPodExists := candidate.Pod.Labels[appsv1alpha1.PodReplacePairNewId]

	// label pod to trigger replace
	replaceTriggered := replaceIndicated || replaceByReplaceUpdate || replaceNewPodExists
	if !replaceTriggered {
		patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%v"}}}`, appsv1alpha1.PodReplaceIndicationLabelKey, true)))
		// add finalizer on origin pod before trigger replace
		if err := controllerutils.AddFinalizer(ctx, c, candidate.Pod, OperationJobReplacePodFinalizer); err != nil {
			return fmt.Errorf("fail to add %s finalizer to origin pod %s/%s : %s", OperationJobReplacePodFinalizer, candidate.Pod.Namespace, candidate.Pod.Name, err.Error())
		}
		if err := c.Patch(ctx, candidate.Pod, patch); err != nil {
			return fmt.Errorf("fail to label origin pod %s/%s with replace indicate label by replaceUpdate: %s", candidate.Pod.Namespace, candidate.Pod.Name, err)
		}
		recorder.Eventf(operationJob, corev1.EventTypeNormal, "ReplaceOriginPod", fmt.Sprintf("Succeeded to trigger originPod %s/%s to replace", operationJob.Namespace, candidate.Pod.Name))
	}

	return nil
}

func (p *PodReplaceHandler) GetOpsProgress(
	ctx context.Context, logger logr.Logger, recorder record.EventRecorder, c client.Client, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) (
	progress ActionProgress, reason string, message string, err error) {

	progress = ActionProgressProcessing
	reason = candidate.OpsStatus.Reason
	message = candidate.OpsStatus.Message

	if candidate.Pod != nil {
		// mark ops status as processing if origin pod exists
		progress = ActionProgressProcessing
		// try to find replaceNewPod
		newPodId, exist := candidate.Pod.Labels[appsv1alpha1.PodReplacePairNewId]
		if exist {
			newPods := corev1.PodList{}
			if err = c.List(ctx, &newPods, client.InNamespace(operationJob.Namespace), client.MatchingLabels{
				appsv1alpha1.PodReplacePairOriginName: candidate.Pod.Name,
			}); err != nil {
				return
			}

			for i := range newPods.Items {
				newPod := newPods.Items[i]
				// do not consider this pod as newPod if pair info don't match
				if newPodId != newPod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
					continue
				}

				// update ops status if newPod exists
				reason = appsv1alpha1.ReasonReplacedByNewPod
				message = newPod.Name
				recorder.Eventf(operationJob, corev1.EventTypeNormal, "ReplaceNewPod", fmt.Sprintf("Succeeded to create newPod %s/%s for originPod %s/%s", operationJob.Namespace, newPod.Name, operationJob.Namespace, candidate.Pod.Name))
				if _, serviceAvailable := newPod.Labels[appsv1alpha1.PodServiceAvailableLabel]; serviceAvailable {
					recorder.Eventf(operationJob, corev1.EventTypeNormal, "ReplaceNewPod", fmt.Sprintf("newPod %s/%s is serviceAvailable, ready to delete originPod %s", operationJob.Namespace, newPod.Name, candidate.Pod.Name))
				}

				// remove replace-protection finalizer from origin pod
				if candidate.Pod.DeletionTimestamp != nil {
					if removeErr := controllerutils.RemoveFinalizer(ctx, c, candidate.Pod, OperationJobReplacePodFinalizer); removeErr != nil {
						err = fmt.Errorf("fail to add %s finalizer to origin pod %s/%s : %s", OperationJobReplacePodFinalizer, candidate.Pod.Namespace, candidate.Pod.Name, removeErr.Error())
					}
				}
				return
			}
		}
	} else {
		if candidate.OpsStatus.Reason == appsv1alpha1.ReasonReplacedByNewPod {
			newPod := &corev1.Pod{}
			if getErr := c.Get(ctx, types.NamespacedName{Namespace: operationJob.Namespace, Name: message}, newPod); getErr != nil {
				err = fmt.Errorf("fail to find replace newPod %s/%s : %s", operationJob.Namespace, message, getErr.Error())
				return
			}
			// mark ops status as succeeded if origin pod is replaced
			progress = ActionProgressSucceeded
		} else {
			// mark ops status as failed if origin pod not found
			progress = ActionProgressFailed
			reason = appsv1alpha1.ReasonPodNotFound
		}
	}
	return
}

func (p *PodReplaceHandler) ReleaseTarget(ctx context.Context, logger logr.Logger, recorder record.EventRecorder, c client.Client, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) error {
	if candidate.Pod == nil || candidate.Pod.DeletionTimestamp != nil {
		return nil
	}

	if _, exist := candidate.Pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; !exist {
		return nil
	}

	// try to remove replace label from origin pod
	patchOperation := map[string]string{
		"op":   "remove",
		"path": fmt.Sprintf("/metadata/labels/%s", strings.ReplaceAll(appsv1alpha1.PodReplaceIndicationLabelKey, "/", "~1")),
	}

	patchBytes, err := json.Marshal([]map[string]string{patchOperation})
	if err != nil {
		return err
	}

	if err := controllerutils.RemoveFinalizer(ctx, c, candidate.Pod, OperationJobReplacePodFinalizer); err != nil {
		return fmt.Errorf("fail to add %s finalizer to origin pod %s/%s : %s", OperationJobReplacePodFinalizer, candidate.Pod.Namespace, candidate.Pod.Name, err.Error())
	}

	return c.Patch(ctx, candidate.Pod, client.RawPatch(types.JSONPatchType, patchBytes))
}
