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

package opscore

import (
	"sort"

	corev1 "k8s.io/api/core/v1"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

type OpsCandidate struct {
	Idx int
	*corev1.Pod
	PodName    string
	Containers []string
	OpsStatus  *appsv1alpha1.OpsStatus
}

const (
	ExtraInfoReleased = "Released"
)

func DecideCandidateByPartition(instance *appsv1alpha1.OperationJob, candidates []*OpsCandidate) []*OpsCandidate {
	if instance.Spec.Partition == nil {
		return candidates
	}
	ordered := activeCandidateToStart(candidates)
	sort.Sort(ordered)

	partition := int(*instance.Spec.Partition)
	if partition >= len(candidates) {
		return candidates
	}
	return candidates[:partition]
}

type activeCandidateToStart []*OpsCandidate

func (o activeCandidateToStart) Len() int {
	return len(o)
}

func (o activeCandidateToStart) Swap(i, j int) {
	o[i], o[j] = o[j], o[i]
}

func (o activeCandidateToStart) Less(i, j int) bool {
	l, r := o[i], o[j]
	lNotStarted := IsCandidateOpsPending(l)
	rNotStarted := IsCandidateOpsPending(r)
	if lNotStarted != rNotStarted {
		return rNotStarted
	}
	return l.Idx < r.Idx
}

func IsCandidateOpsPending(candidate *OpsCandidate) bool {
	if candidate.OpsStatus == nil || candidate.OpsStatus.Progress == "" {
		return true
	}
	return candidate.OpsStatus.Progress == appsv1alpha1.OperationProgressPending
}

func IsCandidateOpsFinished(candidate *OpsCandidate) bool {
	if candidate.OpsStatus == nil || candidate.OpsStatus.Progress == "" {
		return false
	}
	return candidate.OpsStatus.Progress == appsv1alpha1.OperationProgressFailed ||
		candidate.OpsStatus.Progress == appsv1alpha1.OperationProgressSucceeded
}

func IsCandidateOpsReleased(candidate *OpsCandidate) bool {
	if candidate.OpsStatus == nil || candidate.OpsStatus.ExtraInfo == nil {
		return false
	}
	if val, exist := candidate.OpsStatus.ExtraInfo[ExtraInfoReleased]; exist && val == "true" {
		return true
	}
	return false
}

func IsCandidateOpsFailed(candidate *OpsCandidate) bool {
	if candidate.OpsStatus == nil || candidate.OpsStatus.Progress == "" {
		return false
	}
	return candidate.OpsStatus.Progress == appsv1alpha1.OperationProgressFailed
}

func IsCandidateServiceAvailable(candidate *OpsCandidate) bool {
	if candidate.Pod == nil || candidate.Pod.Labels == nil {
		return false
	}
	_, serviceAvailable := candidate.Pod.Labels[appsv1alpha1.PodServiceAvailableLabel]
	return serviceAvailable
}

func MarkCandidateFailed(candidate *OpsCandidate) {
	if candidate.OpsStatus.Progress != appsv1alpha1.OperationProgressSucceeded {
		candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
	}
}

func MarkCandidateReleased(candidate *OpsCandidate) {
	if candidate.OpsStatus == nil {
		candidate.OpsStatus = &appsv1alpha1.OpsStatus{}
	}
	if candidate.OpsStatus.ExtraInfo == nil {
		candidate.OpsStatus.ExtraInfo = map[string]string{}
	}
	candidate.OpsStatus.ExtraInfo[ExtraInfoReleased] = "true"
}
