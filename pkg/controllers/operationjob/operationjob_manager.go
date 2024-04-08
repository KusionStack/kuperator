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

	"github.com/go-logr/logr"
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
)

type TargetOperator interface {
	ListTargets() ([]*OpsCandidate, error)
	OperateTarget(*OpsCandidate) error
	FulfilPodOpsStatus(*OpsCandidate) error
}

type GenericOperator struct {
	ctx          context.Context
	logger       logr.Logger
	client       client.Client
	recorder     record.EventRecorder
	operationJob *appsv1alpha1.OperationJob
}

type OpsCandidate struct {
	pod          *corev1.Pod
	podName      string
	containers   []string
	podOpsStatus *appsv1alpha1.PodOpsStatus

	replaceTriggered bool
	replaceNewPod    *corev1.Pod
	collaSet         *appsv1alpha1.CollaSet
}

func (r *ReconcileOperationJob) newOperator(ctx context.Context, instance *appsv1alpha1.OperationJob, logger logr.Logger) TargetOperator {
	mixin := r.ReconcilerMixin
	genericOperator := &GenericOperator{client: mixin.Client, ctx: ctx, operationJob: instance, logger: logger, recorder: mixin.Recorder}

	switch instance.Spec.Action {
	case appsv1alpha1.OpsActionRestart:
		if GetRecreateHandler() == nil {
			// use Kruise ContainerRecreateRequest to recreate container by default
			recreateHandler := &ContainerRecreateRequestHandler{ctx: ctx, client: mixin.Client, operationJob: instance,
				crrMap: make(map[string]*kruisev1alpha1.ContainerRecreateRequest)}
			return &containerRestartOperator{GenericOperator: genericOperator, handler: recreateHandler}
		}
		return &containerRestartOperator{GenericOperator: genericOperator, handler: GetRecreateHandler()}
	case appsv1alpha1.OpsActionReplace:
		return &podReplaceOperator{GenericOperator: genericOperator,
			podControl: podcontrol.NewRealPodControl(r.ReconcilerMixin.Client, r.ReconcilerMixin.Scheme)}
	default:
		panic(fmt.Errorf("unsupported operation type %s", instance.Spec.Action))
	}
}

func isCandidateOpsFinished(candidate *OpsCandidate) bool {
	if candidate.podOpsStatus == nil || candidate.podOpsStatus.Phase == "" {
		return false
	}
	return candidate.podOpsStatus.Phase == appsv1alpha1.PodPhaseCompleted ||
		candidate.podOpsStatus.Phase == appsv1alpha1.PodPhaseFailed
}
