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

package operationjob

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	. "kusionstack.io/kuperator/pkg/controllers/operationjob/opscore"
	"kusionstack.io/kuperator/pkg/controllers/operationjob/replace"
	ojutils "kusionstack.io/kuperator/pkg/controllers/operationjob/utils"
	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
)

// RegisterOperationJobActions register actions for operationJob
func RegisterOperationJobActions() {
	RegisterAction(appsv1alpha1.OpsActionReplace, &replace.PodReplaceHandler{}, false)
}

// getActionHandler get actions registered for operationJob
func (r *ReconcileOperationJob) getActionHandler(operationJob *appsv1alpha1.OperationJob) (ActionHandler, bool, error) {
	action := operationJob.Spec.Action
	handler, enablePodOpsLifecycle := GetActionResources(action)
	if handler == nil {
		errMsg := fmt.Sprintf("unsupported operation type! please register handler for action: %s", action)
		r.Recorder.Eventf(operationJob, corev1.EventTypeWarning, "OpsAction", errMsg)
		return nil, false, fmt.Errorf(errMsg)
	}

	return handler, enablePodOpsLifecycle, nil
}

func (r *ReconcileOperationJob) listTargets(ctx context.Context, operationJob *appsv1alpha1.OperationJob) ([]*OpsCandidate, error) {
	var candidates []*OpsCandidate
	podOpsStatusMap := ojutils.MapOpsStatusByPod(operationJob)
	for idx := range operationJob.Spec.Targets {
		target := operationJob.Spec.Targets[idx]
		var candidate OpsCandidate
		var pod corev1.Pod

		candidate.Idx = idx
		// fulfil target pod
		candidate.PodName = target.Name
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: operationJob.Namespace, Name: target.Name}, &pod)
		if err == nil {
			candidate.Pod = &pod
		} else if errors.IsNotFound(err) {
			candidate.Pod = nil
		} else {
			return candidates, err
		}

		// fulfil target containers
		candidate.Containers = target.Containers
		if len(target.Containers) == 0 && candidate.Pod != nil {
			var containers []string
			for _, container := range candidate.Pod.Spec.Containers {
				containers = append(containers, container.Name)
			}
			candidate.Containers = containers
		}

		// fulfil or initialize opsStatus
		if opsStatus, exist := podOpsStatusMap[target.Name]; exist {
			candidate.OpsStatus = opsStatus
		} else {
			candidate.OpsStatus = &appsv1alpha1.OpsStatus{
				Name:      target.Name,
				Progress:  appsv1alpha1.OperationProgressPending,
				ExtraInfo: make(map[string]string),
			}
		}

		candidates = append(candidates, &candidate)
	}
	return candidates, nil
}

func (r *ReconcileOperationJob) filterAllowOpsTargets(
	candidates []*OpsCandidate,
	enablePodOpsLifecycle bool,
	operationJob *appsv1alpha1.OperationJob) (allowOpsCandidates []*OpsCandidate, err error) {

	for i := range candidates {
		candidate := candidates[i]

		if IsCandidateOpsFinished(candidate) {
			continue
		}

		if enablePodOpsLifecycle && candidate.Pod == nil {
			continue
		}

		var isDuringOps, isAllowedOps bool
		lifecycleAdapter := NewLifecycleAdapter(operationJob.Name, operationJob.Spec.Action)
		if enablePodOpsLifecycle {
			isDuringOps = podopslifecycle.IsDuringOps(lifecycleAdapter, candidate.Pod)
			_, isAllowedOps = podopslifecycle.AllowOps(lifecycleAdapter, ptr.Deref(operationJob.Spec.OperationDelaySeconds, 0), candidate.Pod)
		} else {
			// if opsLifecycle not enabled, target pod is always allowOps
			isDuringOps = false
			isAllowedOps = true
		}

		if IsCandidateOpsPending(candidate) {
			if enablePodOpsLifecycle {
				if isDuringOps {
					candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
				} else {
					r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "PodOpsLifecycle", "try to begin PodOpsLifecycle for %s", operationJob.Spec.Action)
					if updated, updateErr := ojutils.BeginOperateLifecycle(r.Client, lifecycleAdapter, candidate.Pod); err != nil {
						err = controllerutils.AggregateErrors([]error{err, updateErr})
						continue
					} else if !updated {
						continue
					} else {
						candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
					}
				}
			} else {
				candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
			}
		}

		if isAllowedOps {
			allowOpsCandidates = append(allowOpsCandidates, candidate)
		}
	}
	return
}

func (r *ReconcileOperationJob) operateTargets(
	ctx context.Context,
	operator ActionHandler,
	candidates []*OpsCandidate,
	enablePodOpsLifecycle bool,
	operationJob *appsv1alpha1.OperationJob) error {

	var opsErr, updateErr error
	// operate targets
	if len(candidates) > 0 {
		opsErr = operator.OperateTargets(ctx, candidates, operationJob)
	}
	// update targets status
	_, updateErr = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		// get action progress
		actionProgress, err := operator.GetOpsProgress(ctx, candidate, operationJob)

		// clean opsLifecycle if action operate finished
		if enablePodOpsLifecycle {
			var err error
			if actionProgress == ActionProgressFailed {
				err = r.cleanCandidateOpsLifecycle(ctx, true, candidate, operationJob)
			} else if actionProgress == ActionProgressSucceeded {
				err = r.cleanCandidateOpsLifecycle(ctx, false, candidate, operationJob)
			}
			if err != nil {
				return err
			}
		}

		// transfer actionProgress to operationProgress
		switch actionProgress {
		case ActionProgressProcessing:
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
		case ActionProgressFailed:
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
		case ActionProgressSucceeded:
			if enablePodOpsLifecycle {
				if IsCandidateServiceAvailable(candidate) {
					candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressSucceeded
				}
			} else {
				candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressSucceeded
			}
		default:
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
		}
		return err
	})
	return controllerutils.AggregateErrors([]error{opsErr, updateErr})
}

func (r *ReconcileOperationJob) ensureActiveDeadlineAndTTL(ctx context.Context, operationJob *appsv1alpha1.OperationJob, logger logr.Logger) (bool, *time.Duration, error) {
	isFailed := operationJob.Status.Progress == appsv1alpha1.OperationProgressFailed
	isSucceeded := operationJob.Status.Progress == appsv1alpha1.OperationProgressSucceeded

	if operationJob.Spec.ActiveDeadlineSeconds != nil {
		if !isFailed && !isSucceeded {
			leftTime := time.Duration(*operationJob.Spec.ActiveDeadlineSeconds)*time.Second - time.Since(operationJob.CreationTimestamp.Time)
			if leftTime > 0 {
				return false, &leftTime, nil
			} else {
				logger.Info("should end but still processing")
				r.Recorder.Eventf(operationJob, corev1.EventTypeNormal, "Timeout", "Try to fail OperationJob for timeout...")
				// mark operationjob and targets failed and release targets
				ojutils.MarkOperationJobFailed(operationJob)
				return false, nil, r.releaseTargets(ctx, operationJob)
			}
		}
	}

	if operationJob.Spec.TTLSecondsAfterFinished != nil {
		if isFailed || isSucceeded {
			leftTime := time.Duration(*operationJob.Spec.TTLSecondsAfterFinished)*time.Second - time.Since(operationJob.Status.EndTimestamp.Time)
			if leftTime > 0 {
				return false, &leftTime, nil
			} else {
				logger.Info("should be deleted but still alive")
				r.Recorder.Eventf(operationJob, corev1.EventTypeNormal, "TTL", "Try to delete OperationJob for TTL...")
				err := r.Client.Delete(ctx, operationJob)
				return true, nil, err
			}
		}
	}

	return false, nil, nil
}

func (r *ReconcileOperationJob) releaseTargets(ctx context.Context, operationJob *appsv1alpha1.OperationJob) error {
	actionHandler, enablePodOpsLifecycle, candidates, err := r.getActionHandlerAndTargets(ctx, operationJob)
	if err != nil {
		return err
	}
	err = actionHandler.ReleaseTargets(ctx, candidates, operationJob)
	for i := range candidates {
		candidate := candidates[i]
		// cancel lifecycle if necessary
		if enablePodOpsLifecycle {
			cleanErr := r.cleanCandidateOpsLifecycle(ctx, true, candidate, operationJob)
			err = controllerutils.AggregateErrors([]error{err, cleanErr})
		}
		// mark candidate as failed if not finished
		if IsCandidateOpsFinished(candidate) {
			return nil
		}
		candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
	}
	return err

}

func (r *ReconcileOperationJob) cleanCandidateOpsLifecycle(ctx context.Context, forced bool, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) error {
	if candidate.Pod == nil {
		return nil
	}

	lifecycleAdapter := NewLifecycleAdapter(operationJob.Name, operationJob.Spec.Action)

	if forced {
		err := ojutils.CancelOpsLifecycle(ctx, r.Client, lifecycleAdapter, candidate.Pod)
		if err != nil {
			return err
		}
		r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, fmt.Sprintf("%sOpsLifecycleCanceled", operationJob.Spec.Action), "pod %s/%s ops canceled", candidate.Pod.Namespace, candidate.Pod.Name)
	} else if podopslifecycle.IsDuringOps(lifecycleAdapter, candidate.Pod) {
		if err := ojutils.FinishOperateLifecycle(r.Client, lifecycleAdapter, candidate.Pod); err != nil {
			return err
		}
		r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, fmt.Sprintf("%sOpsLifecycleFinished", operationJob.Spec.Action), "pod %s/%s ops finished", candidate.Pod.Namespace, candidate.Pod.Name)
	}
	return nil
}
