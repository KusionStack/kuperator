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

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
)

type ContainerRestartControl struct {
	*OperateInfo
}

func (p *ContainerRestartControl) OperateTarget(candidate *OpsCandidate) error {
	// skip if containers do not exist
	if _, containerNotFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers); containerNotFound {
		return nil
	}

	// get restart handler from pod's Anno, to do restart
	handler, message := GetRestartHandlerFromPod(candidate.Pod)
	if handler == nil {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonInvalidRestartMethod, message)
		return nil
	}

	return handler.DoRestartContainers(p.Context, p.Client, p.OperationJob, candidate, candidate.Containers)
}

func (p *ContainerRestartControl) FulfilTargetOpsStatus(candidate *OpsCandidate) error {
	if candidate.Pod == nil {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonPodNotFound, "")
		return nil
	}

	if containers, notFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers); notFound {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonContainerNotFound, fmt.Sprintf("Container named %v not found", containers))
		return nil
	}

	// get restart handler from pod's Anno, to do restart
	handler, message := GetRestartHandlerFromPod(candidate.Pod)
	if handler == nil {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonInvalidRestartMethod, message)
		return nil
	}

	// calculate restart progress of podOpsStatus
	candidate.OpsStatus.Progress = handler.GetRestartProgress(p.Context, p.Client, p.OperationJob, candidate)
	return nil
}

func (p *ContainerRestartControl) ReleaseTarget(candidate *OpsCandidate) error {
	if candidate.Pod == nil {
		return nil
	}

	// get restart handler from pod's Anno, to do restart
	handler, message := GetRestartHandlerFromPod(candidate.Pod)
	if handler == nil {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonInvalidRestartMethod, message)
		return nil
	}

	// release target
	if err := handler.ReleasePod(p.Context, p.Client, p.OperationJob, candidate); err != nil {
		return err
	}

	return nil
}
