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

package opscontrol

import (
	"sort"

	corev1 "k8s.io/api/core/v1"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type OpsCandidate struct {
	*corev1.Pod
	PodName      string
	Containers   []string
	PodOpsStatus *appsv1alpha1.PodOpsStatus

	ReplaceTriggered bool
	ReplaceNewPod    *corev1.Pod
	*appsv1alpha1.CollaSet
}

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
	return true
}

func IsCandidateOpsPending(candidate *OpsCandidate) bool {
	if candidate.PodOpsStatus == nil || candidate.PodOpsStatus.Progress == "" {
		return true
	}
	return candidate.PodOpsStatus.Progress == appsv1alpha1.OperationProgressPending
}

func IsCandidateOpsFinished(candidate *OpsCandidate) bool {
	if candidate.PodOpsStatus == nil || candidate.PodOpsStatus.Progress == "" {
		return false
	}
	return candidate.PodOpsStatus.Progress == appsv1alpha1.OperationProgressFailed ||
		candidate.PodOpsStatus.Progress == appsv1alpha1.OperationProgressSucceeded
}

func MarkCandidateAsFailed(candidate *OpsCandidate, reason string) {
	if candidate.PodOpsStatus.ExtraInfo == nil {
		candidate.PodOpsStatus.ExtraInfo = make(map[appsv1alpha1.ExtraInfoKey]string)
	}
	candidate.PodOpsStatus.Progress = appsv1alpha1.OperationProgressFailed
	candidate.PodOpsStatus.ExtraInfo[appsv1alpha1.Reason] = reason
}
