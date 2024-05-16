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

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/utils"
)

var registry map[string]RestartHandler

const (
	CRR string = "ContainerRecreateRequest"
)

const (
	PodPhasePending    appsv1alpha1.PodPhase = "Pending"
	PodPhaseRecreating appsv1alpha1.PodPhase = "Recreating"
	PodPhaseSucceeded  appsv1alpha1.PodPhase = "Succeeded"
)

type containerRestartOperator struct {
	*GenericOperator
	handler RestartHandler
}

type RestartHandler interface {
	DoRestartContainers(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod, containers []string) error
	IsRestartFinished(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod) bool
	GetContainerOpsPhase(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod, container string) appsv1alpha1.ContainerPhase
	FulfilExtraInfo(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, pod *corev1.Pod, extraInfo *map[string]string)
}

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

func (p *containerRestartOperator) ListTargets() ([]*OpsCandidate, error) {
	var candidates []*OpsCandidate
	podOpsStatusMap := ojutils.MapOpsStatusByPod(p.operationJob)
	for _, target := range p.operationJob.Spec.Targets {
		var candidate OpsCandidate
		var pod corev1.Pod

		// fulfil pod
		candidate.podName = target.PodName
		err := p.client.Get(p.ctx, types.NamespacedName{Namespace: p.operationJob.Namespace, Name: target.PodName}, &pod)
		if err != nil {
			return candidates, err
		}
		candidate.pod = &pod

		// fulfil containers
		candidate.containers = target.Containers
		if len(target.Containers) == 0 {
			// restart all containers
			var containers []string
			for _, container := range candidate.pod.Spec.Containers {
				containers = append(containers, container.Name)
			}
			candidate.containers = containers
		}

		// fulfil or initialize opsStatus
		if opsStatus, exist := podOpsStatusMap[target.PodName]; exist {
			candidate.podOpsStatus = opsStatus
		} else {
			candidate.podOpsStatus = &appsv1alpha1.PodOpsStatus{
				PodName:   target.PodName,
				Phase:     appsv1alpha1.PodPhaseNotStarted,
				ExtraInfo: map[string]string{},
			}
			var containerDetails []appsv1alpha1.ContainerOpsStatus
			for _, cName := range candidate.containers {
				containerDetail := appsv1alpha1.ContainerOpsStatus{
					ContainerName: cName,
					Phase:         appsv1alpha1.ContainerPhasePending,
				}
				containerDetails = append(containerDetails, containerDetail)
			}
			candidate.podOpsStatus.ContainerDetails = containerDetails
		}

		candidates = append(candidates, &candidate)
	}
	return candidates, nil
}

func (p *containerRestartOperator) OperateTarget(candidate *OpsCandidate) (err error) {
	if isCandidateOpsFinished(candidate) {
		return nil
	}

	if isCandidateOpsNotStarted(candidate) {
		candidate.podOpsStatus.Phase = appsv1alpha1.PodPhaseStarted
	}

	// if Pod is not begin a Recreate PodOpsLifecycle, trigger it
	isDuringOps := podopslifecycle.IsDuringOps(ojutils.RecreateOpsLifecycleAdapter, candidate.pod)
	if !isDuringOps {
		p.recorder.Eventf(candidate.pod, corev1.EventTypeNormal, "ConainerRecreateLifecycle", "try to begin PodOpsLifecycle for recreating Container of Pod")
		if updated, err := podopslifecycle.Begin(p.client, ojutils.RecreateOpsLifecycleAdapter, candidate.pod); err != nil {
			return fmt.Errorf("fail to begin PodOpsLifecycle for recreating Container of Pod %s/%s: %s", candidate.pod.Namespace, candidate.pod.Name, err)
		} else if updated {
			if err := ojutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(candidate.pod), candidate.pod.ResourceVersion); err != nil {
				return err
			}
		}
	}

	// if Pod is allowed to recreate, try to do restart
	_, allowed := podopslifecycle.AllowOps(ojutils.RecreateOpsLifecycleAdapter, 0, candidate.pod)
	if allowed {
		err := p.handler.DoRestartContainers(p.ctx, p.client, p.operationJob, candidate.pod, candidate.containers)
		if err != nil {
			return err
		}
	}

	// if CRR completed, try to finish Recreate PodOpsLifeCycle
	finished := p.handler.IsRestartFinished(p.ctx, p.client, p.operationJob, candidate.pod)
	if finished && isDuringOps {
		if updated, err := podopslifecycle.Finish(p.client, ojutils.RecreateOpsLifecycleAdapter, candidate.pod); err != nil {
			return fmt.Errorf("failed to finish PodOpsLifecycle for updating Pod %s/%s: %s", candidate.pod.Namespace, candidate.pod.Name, err)
		} else if updated {
			// add an expectation for this pod update, before next reconciling
			if err := ojutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(candidate.pod), candidate.pod.ResourceVersion); err != nil {
				return err
			}
			p.recorder.Eventf(candidate.pod, corev1.EventTypeNormal, "RecreateFinished", "pod %s/%s recreate finished", candidate.pod.Namespace, candidate.pod.Name)
		}
	} else {
		p.recorder.Eventf(candidate.pod, corev1.EventTypeNormal, "WaitingRecreateFinished", "waiting for pod %s/%s to recreate finished", candidate.pod.Namespace, candidate.pod.Name)
	}

	return
}

func (p *containerRestartOperator) FulfilPodOpsStatus(candidate *OpsCandidate) error {
	if isCandidateOpsFinished(candidate) {
		return nil
	}

	// fulfil extraInfo
	if candidate.podOpsStatus.ExtraInfo == nil {
		candidate.podOpsStatus.ExtraInfo = make(map[string]string)
		p.handler.FulfilExtraInfo(p.ctx, p.client, p.operationJob, candidate.pod, &candidate.podOpsStatus.ExtraInfo)
	}

	// calculate restart progress of containerOpsStatus
	recreatingCount := 0
	succeedCount := 0
	completedCount := 0
	failedCount := 0
	var containerDetails []appsv1alpha1.ContainerOpsStatus
	for i := range candidate.podOpsStatus.ContainerDetails {
		containerDetail := candidate.podOpsStatus.ContainerDetails[i]
		containerDetail.Phase = p.handler.GetContainerOpsPhase(p.ctx, p.client, p.operationJob, candidate.pod, containerDetail.ContainerName)
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
	candidate.podOpsStatus.ContainerDetails = containerDetails

	// calculate restart progress of podOpsStatus
	phase := PodPhasePending
	containerCount := len(candidate.containers)
	if failedCount == containerCount {
		phase = appsv1alpha1.PodPhaseFailed
	} else if completedCount == containerCount {
		phase = appsv1alpha1.PodPhaseCompleted
	} else if succeedCount == containerCount {
		phase = PodPhaseSucceeded
	} else if recreatingCount > 0 {
		phase = PodPhaseRecreating
	}
	candidate.podOpsStatus.Phase = phase

	return nil
}

type ContainerRecreateRequestHandler struct {
}

func (h *ContainerRecreateRequestHandler) DoRestartContainers(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob, pod *corev1.Pod, containers []string) error {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, pod.Name)
	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if errors.IsNotFound(err) {
		var crrContainers []kruisev1alpha1.ContainerRecreateRequestContainer
		for _, container := range containers {
			crrContainer := kruisev1alpha1.ContainerRecreateRequestContainer{
				Name: container,
			}
			crrContainers = append(crrContainers, crrContainer)
		}

		crr = &kruisev1alpha1.ContainerRecreateRequest{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: instance.Namespace,
				Name:      fmt.Sprintf("%s-%s", instance.Name, pod.Name),
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(instance, appsv1alpha1.GroupVersion.WithKind("OperationJob")),
				},
			},
			Spec: kruisev1alpha1.ContainerRecreateRequestSpec{
				PodName:    pod.Name,
				Containers: crrContainers,
			},
		}

		if err = client.Create(ctx, crr); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

func (h *ContainerRecreateRequestHandler) IsRestartFinished(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob, pod *corev1.Pod) bool {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, pod.Name)
	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if err != nil {
		return false
	}
	return crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestCompleted
}

func (h *ContainerRecreateRequestHandler) GetContainerOpsPhase(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob,
	pod *corev1.Pod, container string) appsv1alpha1.ContainerPhase {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, pod.Name)
	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if err != nil {
		return appsv1alpha1.ContainerPhasePending
	}

	for _, state := range crr.Status.ContainerRecreateStates {
		if state.Name == container {
			return parsePhaseByCrrPhase(crr.Status.Phase)
		}
	}
	return appsv1alpha1.ContainerPhasePending
}

func (h *ContainerRecreateRequestHandler) FulfilExtraInfo(
	ctx context.Context, client client.Client,
	instance *appsv1alpha1.OperationJob,
	pod *corev1.Pod, extraInfo *map[string]string) {
	crr := &kruisev1alpha1.ContainerRecreateRequest{}
	crrName := fmt.Sprintf("%s-%s", instance.Name, pod.Name)
	err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: crrName}, crr)
	if err != nil {
		return
	}
	(*extraInfo)[CRR] = crr.Name
}

func parsePhaseByCrrPhase(phase kruisev1alpha1.ContainerRecreateRequestPhase) appsv1alpha1.ContainerPhase {
	switch phase {
	case kruisev1alpha1.ContainerRecreateRequestPending:
		return appsv1alpha1.ContainerPhasePending
	case kruisev1alpha1.ContainerRecreateRequestRecreating:
		return appsv1alpha1.ContainerPhaseRecreating
	case kruisev1alpha1.ContainerRecreateRequestSucceeded:
		return appsv1alpha1.ContainerPhaseSucceed
	case kruisev1alpha1.ContainerRecreateRequestFailed:
		return appsv1alpha1.ContainerPhaseFailed
	case kruisev1alpha1.ContainerRecreateRequestCompleted:
		return appsv1alpha1.ContainerPhaseCompleted
	default:
		return appsv1alpha1.ContainerPhasePending
	}
}
