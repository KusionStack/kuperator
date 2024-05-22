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

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	"kusionstack.io/operating/pkg/controllers/operationjob/recreate"
	"kusionstack.io/operating/pkg/controllers/operationjob/replace"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
)

func (r *ReconcileOperationJob) newOperator(ctx context.Context, instance *appsv1alpha1.OperationJob, logger logr.Logger) ActionOperator {
	mixin := r.ReconcilerMixin
	operateInfo := &OperateInfo{Client: mixin.Client, Context: ctx, OperationJob: instance, Logger: logger, Recorder: mixin.Recorder}

	switch instance.Spec.Action {
	case appsv1alpha1.OpsActionRecreate:
		recreateMethodAnno := instance.ObjectMeta.Annotations[appsv1alpha1.AnnotationOperationJobRecreateMethod]
		if recreateMethodAnno == "" || recreate.GetRecreateHandler(recreateMethodAnno) == nil {
			// use Kruise ContainerRecreateRequest to recreate container by default
			return &recreate.ContainerRecreateControl{OperateInfo: operateInfo, Handler: recreate.GetRecreateHandler(recreate.KruiseCcontainerRecreateRequest)}
		}
		return &recreate.ContainerRecreateControl{OperateInfo: operateInfo, Handler: recreate.GetRecreateHandler(recreateMethodAnno)}
	case appsv1alpha1.OpsActionReplace:
		return &replace.PodReplaceControl{OperateInfo: operateInfo,
			PodControl: podcontrol.NewRealPodControl(r.ReconcilerMixin.Client, r.ReconcilerMixin.Scheme)}
	default:
		panic(fmt.Errorf("unsupported operation type %s", instance.Spec.Action))
	}
}

func (r *ReconcileOperationJob) ensureActiveDeadlineOrTTL(ctx context.Context, instance *appsv1alpha1.OperationJob, logger logr.Logger) (bool, *time.Duration, error) {
	isFailed := instance.Status.Progress == appsv1alpha1.OperationProgressFailed
	isSucceeded := instance.Status.Progress == appsv1alpha1.OperationProgressSucceeded

	if instance.Spec.ActiveDeadlineSeconds != nil {
		if !isFailed && !isSucceeded {
			leftTime := time.Duration(*instance.Spec.ActiveDeadlineSeconds)*time.Second - time.Since(instance.CreationTimestamp.Time)
			if leftTime > 0 {
				return false, &leftTime, nil
			} else {
				logger.Info("should end but still processing")
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "Timeout", "Try to fail OperationJob for timeout...")
				ojutils.MarkOperationJobFailed(instance)
				return false, nil, nil
			}
		}
	}

	if instance.Spec.TTLSecondsAfterFinished != nil {
		if isFailed || isSucceeded {
			leftTime := time.Duration(*instance.Spec.TTLSecondsAfterFinished)*time.Second - time.Since(instance.Status.EndTimestamp.Time)
			if leftTime > 0 {
				return false, &leftTime, nil
			} else {
				logger.Info("should be deleted but still alive")
				r.Recorder.Eventf(instance, corev1.EventTypeNormal, "TTL", "Try to delete OperationJob for TTL...")
				err := r.Client.Delete(ctx, instance)
				return true, nil, err
			}
		}
	}

	return false, nil, nil
}

func (r *ReconcileOperationJob) ReleaseTargetsForDeletion(ctx context.Context, instance *appsv1alpha1.OperationJob, logger logr.Logger) error {
	ojutils.MarkOperationJobFailed(instance)
	operator := r.newOperator(ctx, instance, logger)
	candidates, err := operator.ListTargets()
	if err != nil {
		return err
	}

	for _, candidate := range candidates {
		if err := operator.ReleaseTarget(candidate); err != nil {
			return err
		}
	}
	return nil
}
