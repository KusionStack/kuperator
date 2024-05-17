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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
)

const (
	PodPhaseNewPodCreating       appsv1alpha1.PodPhase = "NewPodCreating"
	PodPhaseNewPodStarted        appsv1alpha1.PodPhase = "NewPodStarted"
	PodPhaseOriginPodTerminating appsv1alpha1.PodPhase = "OriginPodTerminating"
)

type podReplaceOperator struct {
	*GenericOperator
	podControl podcontrol.Interface
}

func (p *podReplaceOperator) ListTargets() ([]*OpsCandidate, error) {
	var candidates []*OpsCandidate
	podOpsStatusMap := ojutils.MapOpsStatusByPod(p.operationJob)
	for _, target := range p.operationJob.Spec.Targets {
		var candidate OpsCandidate
		var originPod, replaceNewPod corev1.Pod
		var replaceIndicated, replaceByReplaceUpdate, replaceNewPodExists bool

		// fulfil origin pod
		candidate.podName = target.PodName
		err := p.client.Get(p.ctx, types.NamespacedName{Namespace: p.operationJob.Namespace, Name: target.PodName}, &originPod)
		if err == nil {
			candidate.pod = &originPod
		} else if errors.IsNotFound(err) {
			candidate.pod = nil
		} else {
			return candidates, err
		}

		// parse replace information from origin pod
		if candidate.pod != nil {
			_, replaceIndicated = candidate.pod.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
			_, replaceByReplaceUpdate = candidate.pod.Labels[appsv1alpha1.PodReplaceByReplaceUpdateLabelKey]
			_, replaceNewPodExists = candidate.pod.Labels[appsv1alpha1.PodReplacePairNewId]
		}

		// fulfil or initialize opsStatus and replaceNewPod
		if opsStatus, exist := podOpsStatusMap[target.PodName]; exist {
			candidate.podOpsStatus = opsStatus
			if newPodName, exist := opsStatus.ExtraInfo[appsv1alpha1.ReplacePodNameKey]; exist {
				err := p.client.Get(p.ctx, types.NamespacedName{Namespace: p.operationJob.Namespace, Name: newPodName}, &replaceNewPod)
				if err != nil {
					return candidates, err
				}
				candidate.replaceNewPod = &replaceNewPod
				replaceNewPodExists = true
			}
		} else {
			candidate.podOpsStatus = &appsv1alpha1.PodOpsStatus{
				PodName:   target.PodName,
				Phase:     appsv1alpha1.PodPhaseNotStarted,
				ExtraInfo: map[appsv1alpha1.ExtraInfoKey]string{},
			}
		}

		// fulfil collaset
		collaset, err := getCollaSetByPod(p.ctx, p.client, p.operationJob, &candidate)
		if err != nil {
			return candidates, err
		}
		candidate.collaSet = collaset

		// fulfil replaceTriggered
		candidate.replaceTriggered = replaceIndicated || replaceByReplaceUpdate || replaceNewPodExists

		candidates = append(candidates, &candidate)
	}

	return candidates, nil
}

func (p *podReplaceOperator) OperateTarget(candidate *OpsCandidate) error {
	if isCandidateOpsFinished(candidate) {
		return nil
	}

	if isCandidateOpsNotStarted(candidate) {
		candidate.podOpsStatus.Phase = appsv1alpha1.PodPhaseStarted
	}

	// label pod to trigger replace if not started
	if !candidate.replaceTriggered && candidate.pod != nil {
		patch := client.RawPatch(types.StrategicMergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"%v"}}}`, appsv1alpha1.PodReplaceIndicationLabelKey, true)))
		if err := p.client.Patch(p.ctx, candidate.pod, patch); err != nil {
			return fmt.Errorf("fail to label origin pod %s/%s with replace indicate label by replaceUpdate: %s", candidate.pod.Namespace, candidate.pod.Name, err)
		}
	}

	return nil
}

func (p *podReplaceOperator) FulfilPodOpsStatus(candidate *OpsCandidate) error {
	// try to fulfil ExtraInfo["ReplaceNewPodName"]
	if candidate.pod != nil && candidate.collaSet != nil {
		newPodId, exist := candidate.pod.Labels[appsv1alpha1.PodReplacePairNewId]
		if exist && candidate.podOpsStatus.ExtraInfo[appsv1alpha1.ReplacePodNameKey] == "" {
			filteredPods, err := p.podControl.GetFilteredPods(candidate.collaSet.Spec.Selector, candidate.collaSet)
			if err != nil {
				return err
			}
			for _, pod := range filteredPods {
				if newPodId == pod.Labels[appsv1alpha1.PodInstanceIDLabelKey] {
					if candidate.podOpsStatus.ExtraInfo == nil {
						candidate.podOpsStatus.ExtraInfo = make(map[appsv1alpha1.ExtraInfoKey]string)
					}
					candidate.replaceNewPod = pod
					candidate.podOpsStatus.ExtraInfo[appsv1alpha1.ReplacePodNameKey] = pod.Name
				}
			}
		}
	}

	// calculate replace progress
	if candidate.replaceNewPod != nil {
		if candidate.replaceNewPod.Status.Phase == corev1.PodRunning {
			candidate.podOpsStatus.Phase = PodPhaseNewPodStarted
		} else {
			candidate.podOpsStatus.Phase = PodPhaseNewPodCreating
		}
		if candidate.pod == nil {
			candidate.podOpsStatus.Phase = appsv1alpha1.PodPhaseCompleted
		} else if candidate.pod.DeletionTimestamp != nil {
			candidate.podOpsStatus.Phase = PodPhaseOriginPodTerminating
		}
	}

	// pod is deleted by others or not exist, mark as completed
	if candidate.pod == nil && candidate.replaceNewPod == nil {
		candidate.podOpsStatus.Phase = appsv1alpha1.PodPhaseCompleted
	}

	return nil
}

func (p *podReplaceOperator) ReleaseTarget(candidate *OpsCandidate) (*time.Duration, error) {
	return nil, nil
}

func getCollaSetByPod(ctx context.Context, client client.Client, instance *appsv1alpha1.OperationJob, candidate *OpsCandidate) (*appsv1alpha1.CollaSet, error) {
	var collaSet appsv1alpha1.CollaSet
	ownedByCollaSet := false

	pod := candidate.pod
	if pod == nil {
		if candidate.replaceNewPod != nil {
			pod = candidate.replaceNewPod
		} else {
			// pod is deleted by others or not exist, just ignore
			return nil, nil
		}
	}

	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind != "CollaSet" {
			continue
		}
		ownedByCollaSet = true
		err := client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: ownerRef.Name}, &collaSet)
		if err != nil {
			return nil, err
		}
	}

	if !ownedByCollaSet {
		return nil, fmt.Errorf("target %s/%s is not owned by collaSet", pod.Namespace, pod.Name)
	}

	return &collaSet, nil
}
