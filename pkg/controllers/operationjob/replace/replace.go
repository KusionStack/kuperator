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
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/source"

	. "kusionstack.io/kuperator/pkg/controllers/operationjob/opscore"
	ojutils "kusionstack.io/kuperator/pkg/controllers/operationjob/utils"
	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

const (
	OperationJobReplacePodFinalizer = "finalizer.operationjob.kusionstack.io/replace-protected"
)

const (
	ExtraInfoNotAllowedToReplaceNewPod = "NotAllowedToReplaceNewPod"
)

var _ ActionHandler = &PodReplaceHandler{}

type PodReplaceHandler struct {
	logger   logr.Logger
	recorder record.EventRecorder
	client   client.Client
}

func (p *PodReplaceHandler) Setup(controller controller.Controller, reconcileMixin *mixin.ReconcilerMixin) error {
	// Setup parameters
	p.logger = reconcileMixin.Logger.WithName(appsv1alpha1.OpsActionReplace)
	p.recorder = reconcileMixin.Recorder
	p.client = reconcileMixin.Client

	// Watch for changes to replace new pods
	return controller.Watch(&source.Kind{Type: &corev1.Pod{}}, &OriginPodHandler{Client: reconcileMixin.Client})
}

func (p *PodReplaceHandler) OperateTargets(ctx context.Context, candidates []*OpsCandidate, operationJob *appsv1alpha1.OperationJob) map[string]error {
	errMap := &sync.Map{}
	_, _ = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) (err error) {
		candidate := candidates[i]
		defer func() {
			errMap.Store(candidate.PodName, err)
		}()
		if candidate.Pod == nil || candidate.Pod.Labels == nil {
			return nil
		}

		// parse replace information from new pod
		if originPodName, replaceOriginPodExists := candidate.Pod.Labels[appsv1alpha1.PodReplacePairOriginName]; replaceOriginPodExists {
			candidate.OpsStatus.ExtraInfo[ExtraInfoNotAllowedToReplaceNewPod] = fmt.Sprintf("Not allowed to replace pod when it is a new pod for origin pod [%s]", originPodName)
			return nil
		} else {
			delete(candidate.OpsStatus.ExtraInfo, ExtraInfoNotAllowedToReplaceNewPod)
		}

		// parse replace information from origin pod
		_, replaceIndicated := candidate.Pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
		_, replaceByReplaceUpdate := candidate.Pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
		_, replaceNewPodExists := candidate.Pod.Labels[appsv1alpha1.PodReplacePairNewId]

		// label pod to trigger replace
		replaceTriggered := replaceIndicated || replaceByReplaceUpdate || replaceNewPodExists
		if !replaceTriggered {
			if err := ojutils.UpdatePodWithRetry(ctx, p.client, candidate.Pod, func(pod *corev1.Pod) {
				// add replace indicate label on origin pod to trigger replace
				pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey] = "true"
				// add finalizer on origin pod before trigger replace
				controllerutil.AddFinalizer(pod, OperationJobReplacePodFinalizer)
			}); err != nil {
				retErr := fmt.Errorf("fail to add %s finalizer or replace indicate label to origin pod %s/%s : %s", OperationJobReplacePodFinalizer, candidate.Pod.Namespace, candidate.Pod.Name, err.Error())
				ojutils.SetOpsStatusError(candidate, ojutils.ReasonUpdateObjectFailed, retErr.Error())
				return retErr
			}
			p.recorder.Eventf(operationJob, corev1.EventTypeNormal, "ReplaceOriginPod", fmt.Sprintf("Succeeded to trigger originPod %s/%s to replace", operationJob.Namespace, candidate.Pod.Name))
		}
		return nil
	})
	return ojutils.ConvertSyncErrMap(errMap)
}

func (p *PodReplaceHandler) GetOpsProgress(ctx context.Context, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) (progress ActionProgress, err error) {
	progress = ActionProgressProcessing

	if candidate.Pod != nil {
		// mark ops status as processing if origin pod exists
		progress = ActionProgressProcessing
		// try to find replaceNewPod
		newPodId, exist := candidate.Pod.Labels[appsv1alpha1.PodReplacePairNewId]
		if exist {
			newPods := corev1.PodList{}
			if err = p.client.List(ctx, &newPods, client.InNamespace(operationJob.Namespace), client.MatchingLabels{
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

				// newPod is terminating, just skip, because there will be an updated newPod created
				_, terminating := newPod.Labels[appsv1alpha1.PodDeletionIndicationLabelKey]
				if terminating || newPod.DeletionTimestamp != nil {
					continue
				}

				// update ops status if newPod exists
				candidate.OpsStatus.ExtraInfo[appsv1alpha1.ExtraInfoReplacedNewPodKey] = newPod.Name
				p.recorder.Eventf(operationJob, corev1.EventTypeNormal, "ReplaceNewPod", fmt.Sprintf("Succeeded to create newPod %s/%s for originPod %s/%s", operationJob.Namespace, newPod.Name, operationJob.Namespace, candidate.Pod.Name))
				if _, serviceAvailable := newPod.Labels[appsv1alpha1.PodServiceAvailableLabel]; serviceAvailable {
					p.recorder.Eventf(operationJob, corev1.EventTypeNormal, "ReplaceNewPod", fmt.Sprintf("newPod %s/%s is serviceAvailable, ready to delete originPod %s", operationJob.Namespace, newPod.Name, candidate.Pod.Name))
				}

				// remove replace-protection finalizer from origin pod
				if candidate.Pod.DeletionTimestamp != nil {
					if removeErr := controllerutils.RemoveFinalizer(ctx, p.client, candidate.Pod, OperationJobReplacePodFinalizer); removeErr != nil {
						err = fmt.Errorf("fail to add %s finalizer to origin pod %s/%s : %s", OperationJobReplacePodFinalizer, candidate.Pod.Namespace, candidate.Pod.Name, removeErr.Error())
						ojutils.SetOpsStatusError(candidate, ojutils.ReasonUpdateObjectFailed, err.Error())
						return
					}
				}
				return
			}
		}
	} else {
		newPodName, exist := candidate.OpsStatus.ExtraInfo[appsv1alpha1.ExtraInfoReplacedNewPodKey]
		if exist {
			newPod := &corev1.Pod{}
			if getErr := p.client.Get(ctx, types.NamespacedName{Namespace: operationJob.Namespace, Name: newPodName}, newPod); getErr != nil {
				err = fmt.Errorf("fail to find replace newPod %s/%s : %s", operationJob.Namespace, newPodName, getErr.Error())
				ojutils.SetOpsStatusError(candidate, ojutils.ReasonGetObjectFailed, err.Error())
				return
			}
			if _, serviceAvailable := newPod.Labels[appsv1alpha1.PodServiceAvailableLabel]; !serviceAvailable {
				return
			}
			// mark ops status as succeeded if origin pod is replaced, and new pod is service available
			ojutils.SetOpsStatusError(candidate, "", "")
			progress = ActionProgressSucceeded
		} else {
			// mark ops status as failed if origin pod not found
			progress = ActionProgressFailed
			ojutils.SetOpsStatusError(candidate, appsv1alpha1.ReasonPodNotFound, "failed to replace a non-exist pod")
		}
	}
	return
}

func (p *PodReplaceHandler) ReleaseTargets(ctx context.Context, candidates []*OpsCandidate, operationJob *appsv1alpha1.OperationJob) map[string]error {
	errMap := &sync.Map{}
	_, _ = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) (err error) {
		candidate := candidates[i]
		defer func() {
			errMap.Store(candidate.PodName, err)
		}()
		if candidate.Pod == nil || candidate.Pod.DeletionTimestamp != nil {
			return nil
		}

		// skip if replace is canceled and finalizer is removed
		if _, exist := candidate.Pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]; !exist &&
			!controllerutils.ContainsFinalizer(candidate.Pod, OperationJobReplacePodFinalizer) {
			return nil
		}

		if err := ojutils.UpdatePodWithRetry(ctx, p.client, candidate.Pod, func(pod *corev1.Pod) {
			// try to remove replace label from origin pod
			delete(pod.Labels, appsv1alpha1.PodReplaceIndicationLabelKey)
			// try to remove finalizer from origin pod
			controllerutil.RemoveFinalizer(pod, OperationJobReplacePodFinalizer)
		}); err != nil {
			retErr := fmt.Errorf("fail to remove %s finalizer or replace indicate label from origin pod %s/%s : %s", OperationJobReplacePodFinalizer, candidate.Pod.Namespace, candidate.Pod.Name, err.Error())
			ojutils.SetOpsStatusError(candidate, ojutils.ReasonUpdateObjectFailed, retErr.Error())
			return retErr
		}
		candidate.OpsStatus.ExtraInfo[ExtraInfoReleased] = "true"
		return nil
	})
	return ojutils.ConvertSyncErrMap(errMap)
}
