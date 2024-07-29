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

package strategy

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
)

type sortedPodInfo struct {
	infos    []*podInfo
	revision string
}

func (p *sortedPodInfo) Len() int      { return len(p.infos) }
func (p *sortedPodInfo) Swap(i, j int) { p.infos[i], p.infos[j] = p.infos[j], p.infos[i] }
func (p *sortedPodInfo) Less(i, j int) bool {
	imatch := p.infos[i].revision == p.revision
	jmatch := p.infos[j].revision == p.revision
	if imatch != jmatch {
		return imatch
	}
	return Compare(p.infos[i], p.infos[j])
}

type effectivePods map[string]*podInfo

type podInfo struct {
	name            string
	namespace       string
	labels          map[string]string
	collaSet        string
	resourceContext string
	instanceId      string
	revision        string
	state           *podState
}

type podState struct {
	IsDeleted                 bool
	IsCollaSetUpdatedRevision bool
	IsPodReady                bool
	NodeName                  string
	Phase                     corev1.PodPhase
	ReadyTime                 *metav1.Time
	MaxContainerRestarts      int
	CreationTimestamp         *metav1.Time
}

func (p *podInfo) InstanceKey() string {
	return p.resourceContext + "/" + p.instanceId
}

func Compare(l, r *podInfo) bool {

	// CollaSetUpdatedRevision < CollaSetOldRevision
	if l.state.IsCollaSetUpdatedRevision != r.state.IsCollaSetUpdatedRevision {
		return l.state.IsCollaSetUpdatedRevision
	}

	// PodDecoration UpdatedRevision < PodDecoration OldRevision
	if l.revision != r.revision {
		// Move the latest version to the front,
		//  ensuring that the Pod selected by the partition is always ahead
		return l.revision > r.revision
	}

	// Unassigned < assigned
	// If only one of the pods is unassigned, the unassigned one is smaller
	if l.state.NodeName != r.state.NodeName && (len(l.state.NodeName) == 0 || len(r.state.NodeName) == 0) {
		return len(l.state.NodeName) == 0
	}
	// PodPending < PodUnknown < PodRunning
	m := map[corev1.PodPhase]int{corev1.PodPending: 0, corev1.PodUnknown: 1, corev1.PodRunning: 2}
	if m[l.state.Phase] != m[r.state.Phase] {
		return m[l.state.Phase] < m[r.state.Phase]
	}
	// Not ready < ready
	// If only one of the pods is not ready, the not ready one is smaller
	if l.state.IsPodReady != r.state.IsPodReady {
		return !l.state.IsPodReady
	}
	// Been ready for empty time < less time < more time
	// If both pods are ready, the latest ready one is smaller
	if l.state.IsPodReady && r.state.IsPodReady && !l.state.ReadyTime.Equal(r.state.ReadyTime) {
		return afterOrZero(l.state.ReadyTime, r.state.ReadyTime)
	}
	// Pods with containers with higher restart counts < lower restart counts
	if l.state.MaxContainerRestarts != r.state.MaxContainerRestarts {
		return l.state.MaxContainerRestarts > r.state.MaxContainerRestarts
	}
	// Empty creation time pods < newer pods < older pods
	if !l.state.CreationTimestamp.Equal(r.state.CreationTimestamp) {
		return afterOrZero(l.state.CreationTimestamp, r.state.CreationTimestamp)
	}
	return l.instanceId < r.instanceId
}

func setPodState(pod *corev1.Pod, info *podInfo) {
	info.state = &podState{}
	info.state.NodeName = pod.Spec.NodeName
	info.state.Phase = pod.Status.Phase
	info.state.IsPodReady = controllerutils.IsPodReady(pod)
	info.state.ReadyTime = podReadyTime(pod).DeepCopy()
	info.state.MaxContainerRestarts = maxContainerRestarts(pod)
	info.state.CreationTimestamp = pod.CreationTimestamp.DeepCopy()
}

func podReadyTime(pod *corev1.Pod) *metav1.Time {
	if controllerutils.IsPodReady(pod) {
		for _, c := range pod.Status.Conditions {
			// we only care about pod ready conditions
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return &c.LastTransitionTime
			}
		}
	}
	return &metav1.Time{}
}

// afterOrZero checks if time t1 is after time t2; if one of them
// is zero, the zero time is seen as after non-zero time.
func afterOrZero(t1, t2 *metav1.Time) bool {
	if t1.Time.IsZero() || t2.Time.IsZero() {
		return t1.Time.IsZero()
	}
	return t1.After(t2.Time)
}

func maxContainerRestarts(pod *corev1.Pod) int {
	var maxRestarts int32
	for _, c := range pod.Status.ContainerStatuses {
		if c.RestartCount > maxRestarts {
			maxRestarts = c.RestartCount
		}
	}
	return int(maxRestarts)
}
