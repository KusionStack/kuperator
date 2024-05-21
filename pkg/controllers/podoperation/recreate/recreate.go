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

package recreate

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	. "kusionstack.io/operating/pkg/controllers/podoperation/opscontrol"
	podoperationutils "kusionstack.io/operating/pkg/controllers/podoperation/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

const (
	PodPhasePending    appsv1alpha1.PodPhase = "Pending"
	PodPhaseRecreating appsv1alpha1.PodPhase = "Recreating"
	PodPhaseSucceeded  appsv1alpha1.PodPhase = "Succeeded"
)

type ContainerRecreateControl struct {
	*OperateInfo
	Handler RestartHandler
}

func (p *ContainerRecreateControl) ListTargets() ([]*OpsCandidate, error) {
	var candidates []*OpsCandidate
	podOpsStatusMap := podoperationutils.MapOpsStatusByPod(p.PodOperation)
	for _, target := range p.PodOperation.Spec.Targets {
		var candidate OpsCandidate
		var pod corev1.Pod

		// fulfil pod
		candidate.PodName = target.PodName
		err := p.Client.Get(p.Context, types.NamespacedName{Namespace: p.PodOperation.Namespace, Name: target.PodName}, &pod)
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
				PodName:   target.PodName,
				Phase:     appsv1alpha1.PodPhaseNotStarted,
				ExtraInfo: map[appsv1alpha1.ExtraInfoKey]string{},
			}
			var containerDetails []appsv1alpha1.ContainerOpsStatus
			for _, cName := range candidate.Containers {
				containerDetail := appsv1alpha1.ContainerOpsStatus{
					ContainerName: cName,
					Phase:         appsv1alpha1.ContainerPhasePending,
				}
				containerDetails = append(containerDetails, containerDetail)
			}
			candidate.PodOpsStatus.ContainerDetails = containerDetails
		}

		candidates = append(candidates, &candidate)
	}
	return candidates, nil
}

func (p *ContainerRecreateControl) OperateTarget(candidate *OpsCandidate) error {
	// mark candidate ops started is not started
	if IsCandidateOpsNotStarted(candidate) {
		candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseStarted
	}

	// skip if candidate ops finished, or pod and containers do not exist
	if IsCandidateOpsFinished(candidate) || candidate.Pod == nil || !podoperationutils.ContainerExistsInPod(candidate.Pod, candidate.Containers) {
		return nil
	}

	isDuringUpdatingOps := podopslifecycle.IsDuringOps(collasetutils.UpdateOpsLifecycleAdapter, candidate.Pod)
	isDuringRecreateOps := podopslifecycle.IsDuringOps(podoperationutils.RecreateOpsLifecycleAdapter, candidate.Pod)

	// if Pod is during UpdateOpsLifecycle, fail this recreation
	if isDuringUpdatingOps {
		MarkCandidateAsFailed(candidate, "Pod is during update")
		podoperationutils.MarkPodOperationFailed(p.PodOperation)
		// release target and cancel RecreateOpsLifecycle if during RecreateOpsLifecycle
		if isDuringRecreateOps {
			err := p.Handler.ReleasePod(p.Context, p.Client, p.PodOperation, candidate)
			if err != nil {
				return err
			}
			p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "CancelContainerRecreate", "try to cancel PodOpsLifecycle for recreating Container of Pod")
			return podoperationutils.CancelOpsLifecycle(p.Context, p.Client, podoperationutils.RecreateOpsLifecycleAdapter, candidate.Pod)
		}
		return nil
	}

	// if Pod is not during RecreateOpsLifecycle, trigger it
	if !isDuringRecreateOps {
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "ConainerRecreateLifecycle", "try to begin PodOpsLifecycle for recreating Container of Pod")
		if err := podoperationutils.BeginRecreateLifecycle(p.Client, podoperationutils.RecreateOpsLifecycleAdapter, candidate.Pod); err != nil {
			return err
		}
	}

	// if Pod is allowed to recreate, try to do restart
	_, allowed := podopslifecycle.AllowOps(podoperationutils.RecreateOpsLifecycleAdapter, 0, candidate.Pod)
	if allowed {
		err := p.Handler.DoRestartContainers(p.Context, p.Client, p.PodOperation, candidate, candidate.Containers)
		if err != nil {
			return err
		}
	}

	// if CRR completed or during updating opsLifecycle, try to finish Recreate PodOpsLifeCycle
	finished := p.Handler.IsRestartFinished(p.Context, p.Client, p.PodOperation, candidate)
	if finished && isDuringRecreateOps {
		if err := podoperationutils.FinishRecreateLifecycle(p.Client, podoperationutils.RecreateOpsLifecycleAdapter, candidate.Pod); err != nil {
			return err
		}
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "RecreateFinished", "pod %s/%s recreate finished", candidate.Pod.Namespace, candidate.Pod.Name)
	} else {
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "WaitingRecreateFinished", "waiting for pod %s/%s to recreate finished", candidate.Pod.Namespace, candidate.Pod.Name)
	}

	return nil
}

func (p *ContainerRecreateControl) FulfilPodOpsStatus(candidate *OpsCandidate) error {
	if IsCandidateOpsFinished(candidate) {
		return nil
	}

	if candidate.PodOpsStatus.ExtraInfo == nil {
		candidate.PodOpsStatus.ExtraInfo = make(map[appsv1alpha1.ExtraInfoKey]string)
	}

	if candidate.Pod == nil {
		MarkCandidateAsFailed(candidate, "Pod not found")
		return nil
	}

	if !podoperationutils.ContainerExistsInPod(candidate.Pod, candidate.Containers) {
		MarkCandidateAsFailed(candidate, "Container not found")
		return nil
	}

	// fulfil extraInfo
	p.Handler.FulfilExtraInfo(p.Context, p.Client, p.PodOperation, candidate, &candidate.PodOpsStatus.ExtraInfo)

	// calculate restart progress of containerOpsStatus
	recreatingCount := 0
	succeedCount := 0
	completedCount := 0
	failedCount := 0
	var containerDetails []appsv1alpha1.ContainerOpsStatus
	for i := range candidate.PodOpsStatus.ContainerDetails {
		containerDetail := candidate.PodOpsStatus.ContainerDetails[i]
		containerDetail.Phase = p.Handler.GetContainerOpsPhase(p.Context, p.Client, p.PodOperation, candidate, containerDetail.ContainerName)
		if containerDetail.Phase == appsv1alpha1.ContainerPhaseRecreating {
			recreatingCount++
		} else if containerDetail.Phase == appsv1alpha1.ContainerPhaseSucceed {
			succeedCount++
		} else if containerDetail.Phase == appsv1alpha1.ContainerPhaseCompleted {
			completedCount++
		} else if containerDetail.Phase == appsv1alpha1.ContainerPhaseFailed {
			failedCount++
		}
		containerDetails = append(containerDetails, containerDetail)
	}
	candidate.PodOpsStatus.ContainerDetails = containerDetails

	// calculate restart progress of podOpsStatus
	phase := PodPhasePending
	containerCount := len(candidate.Containers)
	if failedCount == containerCount {
		phase = appsv1alpha1.PodPhaseFailed
	} else if completedCount == containerCount {
		phase = appsv1alpha1.PodPhaseCompleted
	} else if succeedCount == containerCount {
		phase = PodPhaseSucceeded
	} else if recreatingCount > 0 {
		phase = PodPhaseRecreating
	}
	candidate.PodOpsStatus.Phase = phase

	return nil
}

func (p *ContainerRecreateControl) ReleaseTarget(candidate *OpsCandidate) error {
	// 1. release target
	if err := p.Handler.ReleasePod(p.Context, p.Client, p.PodOperation, candidate); err != nil {
		return err
	}

	// 2. cancel lifecycle if pod is during recreate lifecycle
	if candidate.Pod != nil && podopslifecycle.IsDuringOps(podoperationutils.RecreateOpsLifecycleAdapter, candidate.Pod) {
		return podoperationutils.CancelOpsLifecycle(p.Context, p.Client, podoperationutils.RecreateOpsLifecycleAdapter, candidate.Pod)
	}
	return nil
}
