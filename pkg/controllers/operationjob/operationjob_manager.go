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
	ctrlutils "kusionstack.io/kuperator/pkg/controllers/utils"
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

// listTargets get real targets from operationJob.Spec.Targets
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

// filterAndOperateAllowOpsTargets get targets which are allowed to operate
func (r *ReconcileOperationJob) filterAndOperateAllowOpsTargets(
	ctx context.Context,
	operator ActionHandler,
	candidates []*OpsCandidate,
	enablePodOpsLifecycle bool,
	operationJob *appsv1alpha1.OperationJob) (opsErr error) {

	allowOpsCandidatesCh := make(chan *OpsCandidate, len(candidates))
	_, _ = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]

		if IsCandidateOpsFinished(candidate) {
			return nil
		}

		if enablePodOpsLifecycle && candidate.Pod == nil {
			return nil
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
					if updated, err := ojutils.BeginOperateLifecycle(r.Client, lifecycleAdapter, candidate.Pod); err != nil {
						opsErr = controllerutils.AggregateErrors([]error{opsErr, err})
						return err
					} else if !updated {
						return nil
					} else {
						candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
					}
				}
			} else {
				candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
			}
			if candidate.OpsStatus.StartTime == nil {
				now := ctrlutils.FormatTimeNow()
				candidate.OpsStatus.StartTime = &now
			}
		}

		if isAllowedOps {
			allowOpsCandidatesCh <- candidate
		}
		return nil
	})
	err := r.operateTargets(ctx, operator, convertChanToList(allowOpsCandidatesCh), operationJob)
	return controllerutils.AggregateErrors([]error{opsErr, err})
}

// operateTargets operate targets which are allowed to operate
func (r *ReconcileOperationJob) operateTargets(
	ctx context.Context,
	operator ActionHandler,
	candidates []*OpsCandidate,
	operationJob *appsv1alpha1.OperationJob) error {
	if len(candidates) == 0 {
		return nil
	}
	return operator.OperateTargets(ctx, candidates, operationJob)
}

func (r *ReconcileOperationJob) getTargetsOpsStatus(
	ctx context.Context,
	operator ActionHandler,
	candidates []*OpsCandidate,
	enablePodOpsLifecycle bool,
	operationJob *appsv1alpha1.OperationJob) error {
	var updateErr error
	_, _ = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		if IsCandidateOpsFinished(candidate) {
			return nil
		}

		if enablePodOpsLifecycle && candidate.Pod == nil {
			return nil
		}

		if IsCandidateOpsPending(candidate) {
			return nil
		}

		// get action progress
		actionProgress, err := operator.GetOpsProgress(ctx, candidate, operationJob)
		if err != nil {
			updateErr = controllerutils.AggregateErrors([]error{updateErr, err})
		}

		// clean opsLifecycle if action operate finished
		if enablePodOpsLifecycle {
			var err error
			if actionProgress == ActionProgressFailed {
				err = r.cleanCandidateOpsLifecycle(ctx, true, candidate, operationJob)
			} else if actionProgress == ActionProgressSucceeded {
				err = r.cleanCandidateOpsLifecycle(ctx, false, candidate, operationJob)
			}
			if err != nil {
				ojutils.SetOpsStatusError(candidate, ojutils.ReasonUpdateObjectFailed, err.Error())
				updateErr = controllerutils.AggregateErrors([]error{updateErr, err})
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
	return updateErr
}

// ensureActiveDeadlineAndTTL calculate time to ActiveDeadlineSeconds and TTLSecondsAfterFinished and release targets
func (r *ReconcileOperationJob) ensureActiveDeadlineAndTTL(ctx context.Context, operationJob *appsv1alpha1.OperationJob, candidates []*OpsCandidate, logger logr.Logger) (bool, *time.Duration, error) {
	if operationJob.Spec.ActiveDeadlineSeconds != nil {
		var allowReleaseCandidates []*OpsCandidate
		for i := range candidates {
			candidate := candidates[i]
			// just skip if target operation already finished, or not started
			if IsCandidateOpsFinished(candidate) || candidate.OpsStatus.StartTime == nil {
				continue
			}
			leftTime := time.Duration(*operationJob.Spec.ActiveDeadlineSeconds)*time.Second - time.Since(candidate.OpsStatus.StartTime.Time)
			if leftTime > 0 {
				return false, &leftTime, nil
			} else {
				logger.Info("should end but still processing")
				r.Recorder.Eventf(operationJob, corev1.EventTypeNormal, "Timeout", "Try to fail OperationJob for timeout...")
				// mark operationjob and targets failed and release targets
				MarkCandidateFailed(candidate)
				allowReleaseCandidates = append(allowReleaseCandidates, candidate)
			}
		}
		if len(allowReleaseCandidates) > 0 {
			releaseErr := r.releaseTargets(ctx, operationJob, allowReleaseCandidates, false)
			operationJob.Status = r.calculateStatus(operationJob, candidates)
			updateErr := r.updateStatus(ctx, operationJob)
			return false, nil, controllerutils.AggregateErrors([]error{releaseErr, updateErr})
		}
	}

	if operationJob.Spec.TTLSecondsAfterFinished != nil {
		if ojutils.IsJobFinished(operationJob) {
			leftTime := time.Duration(*operationJob.Spec.TTLSecondsAfterFinished)*time.Second - time.Since(operationJob.Status.EndTime.Time)
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

// releaseTargets try to release the targets from operation when the operationJob is deleted
func (r *ReconcileOperationJob) releaseTargets(ctx context.Context, operationJob *appsv1alpha1.OperationJob, candidates []*OpsCandidate, needUpdateStatus bool) error {
	actionHandler, enablePodOpsLifecycle, err := r.getActionHandler(operationJob)
	if err != nil {
		return err
	}
	releaseErr := actionHandler.ReleaseTargets(ctx, candidates, operationJob)
	_, _ = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		// cancel lifecycle if necessary
		if enablePodOpsLifecycle {
			err = r.cleanCandidateOpsLifecycle(ctx, true, candidate, operationJob)
			releaseErr = controllerutils.AggregateErrors([]error{releaseErr, err})
		}
		// mark candidate as failed if not finished
		if !IsCandidateOpsFinished(candidate) {
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
		}
		return nil
	})
	if !needUpdateStatus {
		return releaseErr
	}
	operationJob.Status = r.calculateStatus(operationJob, candidates)
	updateErr := r.updateStatus(ctx, operationJob)
	return controllerutils.AggregateErrors([]error{releaseErr, updateErr})
}

// cleanCandidateOpsLifecycle finishes lifecycle resources from target pod if forced==true gracefully, otherwise remove with force
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

func convertChanToList(ch chan *OpsCandidate) []*OpsCandidate {
	var l []*OpsCandidate
	close(ch)
	for c := range ch {
		l = append(l, c)
	}
	return l
}
