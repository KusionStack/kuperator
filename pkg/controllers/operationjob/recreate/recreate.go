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
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/utils"
)

const (
	PodPhasePending    appsv1alpha1.PodPhase = "Pending"
	PodPhaseRecreating appsv1alpha1.PodPhase = "Recreating"
	PodPhaseSucceeded  appsv1alpha1.PodPhase = "Succeeded"
)

type ContainerRestartOperator struct {
	*OperateInfo
	Handler RestartHandler
}

type RestartHandler interface {
	DoRestartContainers(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod, containers []string) error
	IsRestartFinished(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod) bool
	GetContainerOpsPhase(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod, container string) appsv1alpha1.ContainerPhase
	FulfilExtraInfo(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod, extraInfo *map[appsv1alpha1.ExtraInfoKey]string)
}

var registry map[string]RestartHandler

func RegisterRecreateHandler(methodName string, handler RestartHandler) {
	if registry == nil {
		registry = make(map[string]RestartHandler)
	}
	registry[methodName] = handler
}

func GetRecreateHandler(methodName string) RestartHandler {
	if _, exist := registry[methodName]; exist {
		return registry[methodName]
	}
	return nil
}

func (p *ContainerRestartOperator) ListTargets() ([]*OpsCandidate, error) {
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

func (p *ContainerRestartOperator) OperateTarget(candidate *OpsCandidate) error {
	if candidate.Pod == nil || IsCandidateOpsFinished(candidate) {
		return nil
	}

	if IsCandidateOpsNotStarted(candidate) {
		candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseStarted
	}

	// skip if container do not exist
	if !ojutils.ContainerExistsInPod(candidate.Pod, candidate.Containers) {
		return nil
	}

	// if Pod is not begin a Recreate PodOpsLifecycle, trigger it
	isDuringOps := podopslifecycle.IsDuringOps(ojutils.RecreateOpsLifecycleAdapter, candidate.Pod)
	if !isDuringOps {
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "ConainerRecreateLifecycle", "try to begin PodOpsLifecycle for recreating Container of Pod")
		if updated, err := podopslifecycle.Begin(p.Client, ojutils.RecreateOpsLifecycleAdapter, candidate.Pod); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle for recreating Container of Pod %s/%s: %s", candidate.Pod.Namespace, candidate.Pod.Name, err)
		} else if updated {
			if err := ojutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(candidate.Pod), candidate.Pod.ResourceVersion); err != nil {
				return err
			}
		}
	}

	// if Pod is allowed to recreate, try to do restart
	_, allowed := podopslifecycle.AllowOps(ojutils.RecreateOpsLifecycleAdapter, 0, candidate.Pod)
	if allowed {
		err := p.Handler.DoRestartContainers(p.Context, p.Client, p.OperationJob, candidate.Pod, candidate.Containers)
		if err != nil {
			return err
		}
	}

	// if CRR completed, try to finish Recreate PodOpsLifeCycle
	finished := p.Handler.IsRestartFinished(p.Context, p.Client, p.OperationJob, candidate.Pod)
	if finished && isDuringOps {
		if updated, err := podopslifecycle.Finish(p.Client, ojutils.RecreateOpsLifecycleAdapter, candidate.Pod); err != nil {
			return fmt.Errorf("failed to finish PodOpsLifecycle for updating Pod %s/%s: %s", candidate.Pod.Namespace, candidate.Pod.Name, err)
		} else if updated {
			// add an expectation for this pod update, before next reconciling
			if err := ojutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(candidate.Pod), candidate.Pod.ResourceVersion); err != nil {
				return err
			}
			p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "RecreateFinished", "pod %s/%s recreate finished", candidate.Pod.Namespace, candidate.Pod.Name)
		}
	} else {
		p.Recorder.Eventf(candidate.Pod, corev1.EventTypeNormal, "WaitingRecreateFinished", "waiting for pod %s/%s to recreate finished", candidate.Pod.Namespace, candidate.Pod.Name)
	}

	return nil
}

func (p *ContainerRestartOperator) FulfilPodOpsStatus(candidate *OpsCandidate) error {
	if IsCandidateOpsFinished(candidate) {
		return nil
	}

	if candidate.PodOpsStatus.ExtraInfo == nil {
		candidate.PodOpsStatus.ExtraInfo = make(map[appsv1alpha1.ExtraInfoKey]string)
	}

	if candidate.Pod == nil {
		candidate.PodOpsStatus.ExtraInfo[appsv1alpha1.Reason] = "Pod not found"
		candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseFailed
		return nil
	}

	if !ojutils.ContainerExistsInPod(candidate.Pod, candidate.Containers) {
		candidate.PodOpsStatus.ExtraInfo[appsv1alpha1.Reason] = "Container not found"
		candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseFailed
		return nil
	}

	// fulfil extraInfo
	p.Handler.FulfilExtraInfo(p.Context, p.Client, p.OperationJob, candidate.Pod, &candidate.PodOpsStatus.ExtraInfo)

	// calculate restart progress of containerOpsStatus
	recreatingCount := 0
	succeedCount := 0
	completedCount := 0
	failedCount := 0
	var containerDetails []appsv1alpha1.ContainerOpsStatus
	for i := range candidate.PodOpsStatus.ContainerDetails {
		containerDetail := candidate.PodOpsStatus.ContainerDetails[i]
		containerDetail.Phase = p.Handler.GetContainerOpsPhase(p.Context, p.Client, p.OperationJob, candidate.Pod, containerDetail.ContainerName)
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

func (p *ContainerRestartOperator) ReleaseTarget(candidate *OpsCandidate) (*time.Duration, error) {
	return nil, nil
}
