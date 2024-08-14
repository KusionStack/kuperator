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

	. "kusionstack.io/operating/pkg/controllers/operationjob/opscore"
	"kusionstack.io/operating/pkg/controllers/operationjob/replace"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
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
	for _, target := range operationJob.Spec.Targets {
		var candidate OpsCandidate
		var pod corev1.Pod

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
				Name:     target.Name,
				Progress: appsv1alpha1.OperationProgressPending,
			}
		}

		candidates = append(candidates, &candidate)
	}
	return candidates, nil
}

func (r *ReconcileOperationJob) operateTargets(
	ctx context.Context,
	operator ActionHandler,
	candidates []*OpsCandidate,
	enablePodOpsLifecycle bool,
	operationJob *appsv1alpha1.OperationJob) error {

	_, opsErr := controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]

		// 0. mark candidate ops Progressing if still Pending
		if IsCandidateOpsPending(candidate) {
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
		}
		if candidate.Pod == nil && enablePodOpsLifecycle {
			return nil
		}

		// 1. get operation stages
		var isOpsFinished bool
		var isDuringOps, isAllowedOps bool
		isOpsFinished = IsCandidateOpsFinished(candidate)
		lifecycleAdapter := NewLifecycleAdapter(operationJob.Name, operationJob.Spec.Action)
		if enablePodOpsLifecycle {
			isDuringOps = podopslifecycle.IsDuringOps(lifecycleAdapter, candidate.Pod)
			_, isAllowedOps = podopslifecycle.AllowOps(lifecycleAdapter, ptr.Deref(operationJob.Spec.OperationDelaySeconds, 0), candidate.Pod)
		} else {
			// just ignore if not have OpsLifecycle, i.e., Replace.
			isAllowedOps = true
			isDuringOps = false
		}

		// 2. begin OpsLifecycle if necessary
		if enablePodOpsLifecycle {
			if !isOpsFinished && !isDuringOps {
				r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "PodOpsLifecycle", "try to begin PodOpsLifecycle for %s", operationJob.Spec.Action)
				if err := ojutils.BeginOperateLifecycle(r.Client, lifecycleAdapter, candidate.Pod); err != nil {
					return err
				}
			}
		}

		// 3. try to do real operation
		if !isOpsFinished && isAllowedOps {
			err := operator.OperateTarget(ctx, candidate, operationJob)
			if err != nil {
				return err
			}
		}

		// 4. end or clean opsLifecycle when enablePodOpsLifecycle=true
		if enablePodOpsLifecycle {
			switch candidate.OpsStatus.Progress {
			case appsv1alpha1.OperationProgressFailed:
				// cancel opsLifecycle if ops failed
				if err := ojutils.CancelOpsLifecycle(ctx, r.Client, lifecycleAdapter, candidate.Pod); err != nil {
					return err
				}
				r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, fmt.Sprintf("%sOpsCanceled", operationJob.Spec.Action), "pod %s/%s ops canceled due to anction failed", candidate.Pod.Namespace, candidate.Pod.Name)
			case appsv1alpha1.OperationProgressFinishingOpsLifecycle:
				// finish opsLifecycle is action done
				if isDuringOps {
					if err := ojutils.FinishOperateLifecycle(r.Client, lifecycleAdapter, candidate.Pod); err != nil {
						return err
					}
					r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, fmt.Sprintf("%sOpsFinished", operationJob.Spec.Action), "pod %s/%s ops finished", candidate.Pod.Namespace, candidate.Pod.Name)
				}
				// mark ops succeeded if candidate is service available when enablePodOpsLifecycle
				if candidate.Pod.Labels != nil && candidate.Pod.Labels[appsv1alpha1.PodServiceAvailableLabel] != "" {
					candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressSucceeded
				}
			case appsv1alpha1.OperationProgressSucceeded:
			default:
			}
		}
		return nil
	})

	return opsErr
}

func (r *ReconcileOperationJob) fulfilTargetsOpsStatus(
	ctx context.Context,
	operator ActionHandler,
	candidates []*OpsCandidate,
	enablePodOpsLifecycle bool,
	operationJob *appsv1alpha1.OperationJob) error {
	_, err := controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		if IsCandidateOpsFinished(candidate) {
			return nil
		}
		actionProgress, err := operator.GetOpsProgress(ctx, candidate, operationJob)
		// transfer ActionProgress to OperationProgress
		switch actionProgress {
		case ActionProgressProcessing:
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
		case ActionProgressFailed:
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
		case ActionProgressSucceeded:
			if enablePodOpsLifecycle {
				candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFinishingOpsLifecycle
			} else {
				candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressSucceeded
			}
		default:
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
		}
		return err
	})
	return err
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
				return false, nil, r.releaseTargets(ctx, logger, operationJob)
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

func (r *ReconcileOperationJob) releaseTargets(ctx context.Context, logger logr.Logger, operationJob *appsv1alpha1.OperationJob) error {
	actionHandler, enablePodOpsLifecycle, candidates, err := r.getActionHandlerAndTargets(ctx, operationJob)
	if err != nil {
		return err
	}

	_, err = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		err := actionHandler.ReleaseTarget(ctx, candidate, operationJob)
		// mark candidate as failed is not succeeded
		if candidate.OpsStatus.Progress != appsv1alpha1.OperationProgressSucceeded {
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
		}
		// cancel lifecycle if pod is during ops lifecycle
		if enablePodOpsLifecycle {
			lifecycleAdapter := NewLifecycleAdapter(operationJob.Name, operationJob.Spec.Action)

			return ojutils.CancelOpsLifecycle(ctx, r.Client, lifecycleAdapter, candidate.Pod)
		}
		return err
	})
	return err
}
