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

package restart

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

type ContainerRestartControl struct {
	*OperateInfo
}

func (p *ContainerRestartControl) ListTargets() ([]*OpsCandidate, error) {
	var candidates []*OpsCandidate
	podOpsStatusMap := ojutils.MapOpsStatusByPod(p.OperationJob)
	for _, target := range p.OperationJob.Spec.Targets {
		var candidate OpsCandidate
		var pod corev1.Pod

		// fulfil pod
		candidate.PodName = target.PodName
		err := p.Client.Get(p.Context, types.NamespacedName{Namespace: p.OperationJob.Namespace, Name: target.PodName}, &pod)
		if err == nil {
			candidate.Pod = &pod
		} else if errors.IsNotFound(err) {
			candidate.Pod = nil
		} else {
			return candidates, err
		}

		// fulfil containers
		candidate.Containers = target.Containers
		if len(target.Containers) == 0 && candidate.Pod != nil {
			// restart all containers
			var containers []string
			for _, container := range candidate.Pod.Spec.Containers {
				containers = append(containers, container.Name)
			}
			candidate.Containers = containers
		}

		// fulfil or initialize opsStatus
		if opsStatus, exist := podOpsStatusMap[target.PodName]; exist {
			candidate.PodOpsStatus = opsStatus
		} else {
			candidate.PodOpsStatus = &appsv1alpha1.PodOpsStatus{
				PodName:  target.PodName,
				Progress: appsv1alpha1.OperationProgressPending,
			}
		}

		candidates = append(candidates, &candidate)
	}
	return candidates, nil
}

func (p *ContainerRestartControl) OperateTarget(candidate *OpsCandidate) error {
	// mark candidate ops started is not started
	if IsCandidateOpsPending(candidate) {
		candidate.PodOpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
	}

	// skip if candidate ops finished, or pod and containers do not exist
	_, containerNotFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers)
	if IsCandidateOpsFinished(candidate) || candidate.Pod == nil || containerNotFound {
		return nil
	}

	// get restart handler from pod's Anno, to do restart
	handler, message := GetRestartHandlerFromPod(candidate.Pod)
	if handler == nil {
		MarkCandidateAsFailed(p.OperationJob, candidate, appsv1alpha1.ReasonInvalidRestartMethod, message)
		return nil
	}

	// if Pod is not during RestartOpsLifecycle, trigger it
	isDuringRestartOps := podopslifecycle.IsDuringOps(ojutils.RestartOpsLifecycleAdapter, candidate.Pod)
	if !isDuringRestartOps {
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "ContainerRestartLifecycle", "try to begin PodOpsLifecycle for recreating Container of Pod")
		if err := ojutils.BeginRestarteLifecycle(p.Client, ojutils.RestartOpsLifecycleAdapter, candidate.Pod); err != nil {
			return err
		}
	}

	// if Pod is allowed to restart, try to do restart
	_, allowed := podopslifecycle.AllowOps(ojutils.RestartOpsLifecycleAdapter, realValue(p.OperationJob.Spec.OperationDelaySeconds), candidate.Pod)
	if allowed {
		err := handler.DoRestartContainers(p.Context, p.Client, p.OperationJob, candidate, candidate.Containers)
		if err != nil {
			return err
		}
	}

	// if CRR completed or during updating opsLifecycle, try to finish Restart PodOpsLifeCycle
	candidate.PodOpsStatus.Progress = handler.GetRestartProgress(p.Context, p.Client, p.OperationJob, candidate)
	if IsCandidateOpsFinished(candidate) && isDuringRestartOps {
		if err := ojutils.FinishRestartLifecycle(p.Client, ojutils.RestartOpsLifecycleAdapter, candidate.Pod); err != nil {
			return err
		}
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "RestartFinished", "pod %s/%s restart finished", candidate.Pod.Namespace, candidate.Pod.Name)
	} else {
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "WaitingRestartFinished", "waiting for pod %s/%s to restart finished", candidate.Pod.Namespace, candidate.Pod.Name)
	}

	return nil
}

func (p *ContainerRestartControl) FulfilPodOpsStatus(candidate *OpsCandidate) error {
	if IsCandidateOpsFinished(candidate) {
		return nil
	}

	if candidate.Pod == nil {
		MarkCandidateAsFailed(p.OperationJob, candidate, appsv1alpha1.ReasonPodNotFound, "")
		return nil
	}

	if containers, notFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers); notFound {
		MarkCandidateAsFailed(p.OperationJob, candidate, appsv1alpha1.ReasonContainerNotFound, fmt.Sprintf("Container named %v not found", containers))
		return nil
	}

	// get restart handler from pod's Anno, to do restart
	handler, message := GetRestartHandlerFromPod(candidate.Pod)
	if handler == nil {
		MarkCandidateAsFailed(p.OperationJob, candidate, appsv1alpha1.ReasonInvalidRestartMethod, message)
		return nil
	}

	// calculate restart progress of podOpsStatus
	candidate.PodOpsStatus.Progress = handler.GetRestartProgress(p.Context, p.Client, p.OperationJob, candidate)
	return nil
}

func (p *ContainerRestartControl) ReleaseTarget(candidate *OpsCandidate) error {
	if candidate.Pod == nil {
		return nil
	}

	// get restart handler from pod's Anno, to do restart
	handler, message := GetRestartHandlerFromPod(candidate.Pod)
	if handler == nil {
		MarkCandidateAsFailed(p.OperationJob, candidate, appsv1alpha1.ReasonInvalidRestartMethod, message)
		return nil
	}

	// release target
	if err := handler.ReleasePod(p.Context, p.Client, p.OperationJob, candidate); err != nil {
		return err
	}

	// cancel lifecycle if pod is during restart lifecycle
	if podopslifecycle.IsDuringOps(ojutils.RestartOpsLifecycleAdapter, candidate.Pod) {
		return ojutils.CancelOpsLifecycle(p.Context, p.Client, ojutils.RestartOpsLifecycleAdapter, candidate.Pod)
	}
	return nil
}

func realValue(val *int32) int32 {
	if val == nil {
		return 0
	}

	return *val
}
