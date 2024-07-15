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

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	"kusionstack.io/operating/pkg/controllers/operationjob/replace"
	"kusionstack.io/operating/pkg/controllers/operationjob/restart"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

func (r *ReconcileOperationJob) newOperator(ctx context.Context, operationJob *appsv1alpha1.OperationJob, logger logr.Logger) (ActionOperator, podopslifecycle.LifecycleAdapter, error) {
	mixin := r.ReconcilerMixin
	operateInfo := &OperateInfo{
		Context:      ctx,
		Logger:       logger,
		Client:       mixin.Client,
		Recorder:     mixin.Recorder,
		OperationJob: operationJob,
	}

	switch operationJob.Spec.Action {
	case appsv1alpha1.OpsActionRestart:
		return &restart.ContainerRestartControl{OperateInfo: operateInfo}, ojutils.RestartOpsLifecycleAdapter, nil
	case appsv1alpha1.OpsActionReplace:
		return &replace.PodReplaceControl{OperateInfo: operateInfo,
			PodControl: podcontrol.NewRealPodControl(r.ReconcilerMixin.Client, r.ReconcilerMixin.Scheme)}, nil, nil
	default:
		r.Recorder.Eventf(operationJob, corev1.EventTypeWarning, "OpsAction", "unsupported operation type!")
		return nil, nil, fmt.Errorf("unsupported operation type %s", operationJob.Spec.Action)
	}
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
		if operationJob.Spec.Action == appsv1alpha1.OpsActionRestart {
			candidate.Containers = target.Containers
			if len(target.Containers) == 0 && candidate.Pod != nil {
				var containers []string
				for _, container := range candidate.Pod.Spec.Containers {
					containers = append(containers, container.Name)
				}
				candidate.Containers = containers
			}
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

		// fulfil Collaset
		collaset, err := ojutils.GetCollaSetByPod(ctx, r.Client, operationJob, &candidate)
		if err != nil {
			return candidates, err
		}
		candidate.CollaSet = collaset

		candidates = append(candidates, &candidate)
	}
	return candidates, nil
}

func (r *ReconcileOperationJob) operateTargets(
	operator ActionOperator,
	candidates []*OpsCandidate,
	lifecycleAdapter podopslifecycle.LifecycleAdapter,
	operationJob *appsv1alpha1.OperationJob) error {

	_, opsErr := controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]

		// 0. mark candidate ops Progressing if still Pending
		if IsCandidateOpsPending(candidate) {
			candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
		}
		if candidate.Pod == nil {
			return nil
		}

		// 1. get operation stages
		var isOpsFinished bool
		var usingOpsLifecycle, isDuringOps, isAllowedOps bool
		isOpsFinished = IsCandidateOpsFinished(candidate)
		if lifecycleAdapter == nil {
			// just ignore if not have OpsLifecycle, i.e., Replace.
			usingOpsLifecycle = false
			isAllowedOps = true
		} else {
			usingOpsLifecycle = true
			isDuringOps = podopslifecycle.IsDuringOps(lifecycleAdapter, candidate.Pod)
			_, isAllowedOps = podopslifecycle.AllowOps(ojutils.RestartOpsLifecycleAdapter, realValue(operationJob.Spec.OperationDelaySeconds), candidate.Pod)
		}

		// 2. begin OpsLifecycle if necessary
		if usingOpsLifecycle && !isOpsFinished && !isDuringOps {
			r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "PodOpsLifecycle", "try to begin PodOpsLifecycle for %s", operationJob.Spec.Action)
			if err := ojutils.BeginOperateLifecycle(r.Client, lifecycleAdapter, candidate.Pod); err != nil {
				return err
			}
		}

		// 3. try to do real operation
		if !isOpsFinished && isAllowedOps {
			err := operator.OperateTarget(candidate)
			if err != nil {
				return err
			}
		}

		// 4. finish OpsLifecycle if operation is done
		if usingOpsLifecycle && isOpsFinished && isDuringOps {
			if err := ojutils.FinishOperateLifecycle(r.Client, ojutils.RestartOpsLifecycleAdapter, candidate.Pod); err != nil {
				return err
			}
			r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, fmt.Sprintf("%sOpsFinished", operationJob.Spec.Action), "pod %s/%s ops finished", candidate.Pod.Namespace, candidate.Pod.Name)
		} else {
			r.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, fmt.Sprintf("Waiting%sOpsFinished", operationJob.Spec.Action), "waiting for pod %s/%s to ops finished", candidate.Pod.Namespace, candidate.Pod.Name)
		}

		return nil
	})

	return opsErr
}

func (r *ReconcileOperationJob) fulfilTargetsOpsStatus(operator ActionOperator, candidates []*OpsCandidate, operationJob *appsv1alpha1.OperationJob) error {
	_, err := controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		if IsCandidateOpsFinished(candidate) {
			return nil
		}
		return operator.FulfilTargetOpsStatus(candidate)
	})
	return err
}

func (r *ReconcileOperationJob) ensureActiveDeadlineOrTTL(ctx context.Context, operationJob *appsv1alpha1.OperationJob, logger logr.Logger) (bool, *time.Duration, error) {
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
				ojutils.MarkOperationJobFailed(operationJob)
				return false, nil, nil
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

func (r *ReconcileOperationJob) ReleaseTargetsForDeletion(ctx context.Context, operationJob *appsv1alpha1.OperationJob, logger logr.Logger) error {
	ojutils.MarkOperationJobFailed(operationJob)
	operator, lifecycleAdapter, err := r.newOperator(ctx, operationJob, logger)
	if err != nil {
		return err
	}

	candidates, err := r.listTargets(ctx, operationJob)
	if err != nil {
		return err
	}

	_, err = controllerutils.SlowStartBatch(len(candidates), controllerutils.SlowStartInitialBatchSize, false, func(i int, _ error) error {
		candidate := candidates[i]
		err := operator.ReleaseTarget(candidate)
		// cancel lifecycle if pod is during ops lifecycle
		if lifecycleAdapter != nil && podopslifecycle.IsDuringOps(lifecycleAdapter, candidate.Pod) {
			return ojutils.CancelOpsLifecycle(ctx, r.Client, lifecycleAdapter, candidate.Pod)
		}
		return err
	})
	return err
}

func realValue(val *int32) int32 {
	if val == nil {
		return 0
	}

	return *val
}
