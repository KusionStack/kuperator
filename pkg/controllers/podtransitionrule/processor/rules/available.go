/*
Copyright 2023 The KusionStack Authors.

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

package rules

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/register"
	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/utils"
)

type AvailableRuler struct {
	Name string

	MinAvailableValue   *intstr.IntOrString
	MaxUnavailableValue *intstr.IntOrString

	Client client.Client
}

// Filter unavailable pods and try approve available pods as much as possible
func (r *AvailableRuler) Filter(podTransitionRule *appsv1alpha1.PodTransitionRule, targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	effectiveTargets := sets.NewString()
	pass := sets.NewString()
	rejects := map[string]string{}
	for _, t := range targets {
		effectiveTargets.Insert(t.Name)
	}
	maxUnavailableQuota := len(effectiveTargets)
	allowUnavailable := maxUnavailableQuota
	minAvailableQuota := 0
	if r.MaxUnavailableValue != nil {
		quota, err := intstr.GetScaledValueFromIntOrPercent(r.MaxUnavailableValue, len(effectiveTargets), true)
		if err != nil {
			return rejectAllWithErr(subjects, pass, rejects, "[%s] fail to get int value from raw max unavailable value(%s), error: %v", r.Name, r.MaxUnavailableValue.String(), err)
		}
		maxUnavailableQuota = quota
		allowUnavailable = quota
	}

	if r.MinAvailableValue != nil {
		quota, err := intstr.GetScaledValueFromIntOrPercent(r.MinAvailableValue, len(effectiveTargets), false)
		if err != nil {
			return rejectAllWithErr(subjects, pass, rejects, "[%s] fail to get int value from raw min available value(%s), error: %v", r.Name, r.MaxUnavailableValue.String(), err)
		}
		minAvailableQuota = quota
	}
	// TODO: UncreatedReplicas
	// allowUnavailable -= uncreatedReplicas
	allAvailableSize := 0
	var minTimeLeft *int64
	// filter unavailable pods
	for podName := range effectiveTargets {
		pod := targets[podName]
		if utils.IsPodPassRule(pod.Name, podTransitionRule, r.Name) {
			allowUnavailable--
			continue
		}

		isUnavailable, timeLeft := processUnavailableFunc(pod)
		if isUnavailable {
			allowUnavailable--
			minTimeLeft = min(minTimeLeft, timeLeft)
			continue
		}
		allAvailableSize++
	}
	rejectByMaxUnavailablePods, keepMinAvailablePods := map[string]*corev1.Pod{}, map[string]*corev1.Pod{}
	// try approve available pod
	for podName := range subjects {
		pod := targets[podName]
		if utils.IsPodPassRule(pod.Name, podTransitionRule, r.Name) {
			pass.Insert(pod.Name)
			continue
		}

		isUnavailable, _ := processUnavailableFunc(pod)
		if isUnavailable {
			pass.Insert(podName)
			continue
		}

		if allowUnavailable > 0 {
			if allAvailableSize-minAvailableQuota < 1 {
				// reject by min available policy
				keepMinAvailablePods[podName] = pod
				continue
			}
			allAvailableSize--
			pass.Insert(podName)
			allowUnavailable--
			continue
		}
		// reject by max unavailable policy
		rejectByMaxUnavailablePods[podName] = pod
	}

	for podName := range keepMinAvailablePods {
		rejects[podName] = fmt.Sprintf("blocked by min available policy: [min available]=%d/%d, [current keep available]=%d/%d", minAvailableQuota, len(effectiveTargets), allAvailableSize, len(effectiveTargets))
	}
	for podName := range rejectByMaxUnavailablePods {
		rejects[podName] = fmt.Sprintf("[%s] blocked by max unavailable policy: [max unavailable]=%d/%d, [current unavailable]=%d/%d", r.Name, maxUnavailableQuota, len(effectiveTargets), len(effectiveTargets)-allAvailableSize, len(effectiveTargets))
	}

	if minTimeLeft != nil {
		interval := time.Duration(*minTimeLeft) * time.Second
		return &FilterResult{Passed: pass, Rejected: rejects, Interval: &interval, Err: fmt.Errorf("[%s] pods not finish warm up until %d seconds later", r.Name, minTimeLeft)}
	}

	return &FilterResult{Passed: pass, Rejected: rejects}
}

func processUnavailableFunc(pod *corev1.Pod) (bool, *int64) {
	isUnavailable := false
	var minInterval *int64
	for _, f := range register.UnAvailableFuncList {
		unavailable, interval := f(pod)
		if !unavailable {
			continue
		}
		isUnavailable = true
		if interval != nil {
			if minInterval == nil || *interval < *minInterval {
				minInterval = interval
			}
		}
	}
	return isUnavailable, minInterval
}

func min(a, b *int64) *int64 {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if *a < *b {
		return a
	}
	return b
}
