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

package replace

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
)

const (
	PodPhaseNewPodCreating       appsv1alpha1.PodPhase = "NewPodCreating"
	PodPhaseNewPodStarted        appsv1alpha1.PodPhase = "NewPodStarted"
	PodPhaseOriginPodTerminating appsv1alpha1.PodPhase = "OriginPodTerminating"
)

type PodReplaceControl struct {
	*OperateInfo
	PodControl podcontrol.Interface
}

func (p *PodReplaceControl) ListTargets() ([]*OpsCandidate, error) {
	var candidates []*OpsCandidate
	podOpsStatusMap := ojutils.MapOpsStatusByPod(p.OperationJob)
	for _, target := range p.OperationJob.Spec.Targets {
		var candidate OpsCandidate
		var originPod, replaceNewPod corev1.Pod
		var replaceIndicated, replaceByReplaceUpdate, replaceNewPodExists bool

		// fulfil origin pod
		candidate.PodName = target.PodName
		err := p.Client.Get(p.Context, types.NamespacedName{Namespace: p.OperationJob.Namespace, Name: target.PodName}, &originPod)
		if err == nil {
			candidate.Pod = &originPod
		} else if errors.IsNotFound(err) {
			candidate.Pod = nil
		} else {
			return candidates, err
		}

		// parse replace information from origin pod
		if candidate.Pod != nil {
			_, replaceIndicated = candidate.Pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			_, replaceByReplaceUpdate = candidate.Pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
			_, replaceNewPodExists = candidate.Pod.Labels[appsv1alpha1.PodReplacePairNewId]
		}

		// fulfil or initialize opsStatus and replaceNewPod
		if opsStatus, exist := podOpsStatusMap[target.PodName]; exist {
			candidate.PodOpsStatus = opsStatus
			if newPodName, exist := opsStatus.ExtraInfo[appsv1alpha1.ReplacePodNameKey]; exist {
				err := p.Client.Get(p.Context, types.NamespacedName{Namespace: p.OperationJob.Namespace, Name: newPodName}, &replaceNewPod)
				if err != nil {
					return candidates, err
				}
				candidate.ReplaceNewPod = &replaceNewPod
				replaceNewPodExists = true
			}
		} else {
			candidate.PodOpsStatus = &appsv1alpha1.PodOpsStatus{
				PodName:   target.PodName,
				Phase:     appsv1alpha1.PodPhaseNotStarted,
				ExtraInfo: map[appsv1alpha1.ExtraInfoKey]string{},
			}
		}

		// fulfil Collaset
		collaset, err := ojutils.GetCollaSetByPod(p.Context, p.Client, p.OperationJob, &candidate)
		if err != nil {
			return candidates, err
		}
		candidate.CollaSet = collaset

		// fulfil ReplaceTriggered
		candidate.ReplaceTriggered = replaceIndicated || replaceByReplaceUpdate || replaceNewPodExists

		candidates = append(candidates, &candidate)
	}

	return candidates, nil
}

func (p *PodReplaceControl) OperateTarget(candidate *OpsCandidate) error {
	// mark candidate ops started is not started
	if IsCandidateOpsNotStarted(candidate) {
		candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseStarted
	}

	// skip if candidate ops finished
	if IsCandidateOpsFinished(candidate) {
		return nil
	}

	// label pod to trigger replace if not started
	if !candidate.ReplaceTriggered && candidate.Pod != nil {
		patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%v"}}}`, appsv1alpha1.PodReplaceIndicationLabelKey, true)))
		if err := p.Client.Patch(p.Context, candidate.Pod, patch); err != nil {
			return fmt.Errorf("fail to label origin pod %s/%s with replace indicate label by replaceUpdate: %s", candidate.Pod.Namespace, candidate.Pod.Name, err)
		}
	}

	return nil
}

func (p *PodReplaceControl) FulfilPodOpsStatus(candidate *OpsCandidate) error {
	// try to fulfil ExtraInfo["ReplaceNewPodName"]
	if candidate.Pod != nil && candidate.CollaSet != nil {
		newPodId, exist := candidate.Pod.Labels[appsv1alpha1.PodReplacePairNewId]
		if exist && candidate.PodOpsStatus.ExtraInfo[appsv1alpha1.ReplacePodNameKey] == "" {
			filteredPods, err := p.PodControl.GetFilteredPods(candidate.CollaSet.Spec.Selector, candidate.CollaSet)
			if err != nil {
				return err
			}
			for _, pod := range filteredPods {
				if newPodId == pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
					if candidate.PodOpsStatus.ExtraInfo == nil {
						candidate.PodOpsStatus.ExtraInfo = make(map[appsv1alpha1.ExtraInfoKey]string)
					}
					candidate.ReplaceNewPod = pod
					candidate.PodOpsStatus.ExtraInfo[appsv1alpha1.ReplacePodNameKey] = pod.Name
				}
			}
		}
	}

	// calculate replace progress
	if candidate.ReplaceNewPod != nil {
		if candidate.ReplaceNewPod.Status.Phase == corev1.PodRunning {
			candidate.PodOpsStatus.Phase = PodPhaseNewPodStarted
		} else {
			candidate.PodOpsStatus.Phase = PodPhaseNewPodCreating
		}
		if candidate.Pod == nil {
			candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseCompleted
		} else if candidate.Pod.DeletionTimestamp != nil {
			candidate.PodOpsStatus.Phase = PodPhaseOriginPodTerminating
		}
	}

	// pod is deleted by others or not exist, mark as completed
	if candidate.Pod == nil && candidate.ReplaceNewPod == nil {
		candidate.PodOpsStatus.Phase = appsv1alpha1.PodPhaseCompleted
	}

	return nil
}

func (p *PodReplaceControl) ReleaseTarget(candidate *OpsCandidate) error {
	return nil
}
