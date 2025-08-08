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
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/register"
	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/utils"
)

type AvailableRuler struct {
	Name   string
	Client client.Client

	MinAvailableValue    *intstr.IntOrString
	MaxUnavailableValue  *intstr.IntOrString
	MinAvailablePolicy   *appsv1alpha1.AdaptivePolicy
	MaxUnavailablePolicy *appsv1alpha1.AdaptivePolicy
}

// Filter unavailable pods and try approve available pods as much as possible
func (r *AvailableRuler) Filter(podTransitionRule *appsv1alpha1.PodTransitionRule, targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	effectiveTargets := sets.NewString()
	pass := sets.NewString()
	rejects := map[string]string{}
	for _, t := range targets {
		effectiveTargets.Insert(t.Name)
	}
	totalCnt := len(effectiveTargets)
	maxUnavailableQuota := totalCnt
	allowUnavailable := maxUnavailableQuota
	minAvailableQuota := 0
	if r.MaxUnavailableValue != nil {
		quota, err := intstr.GetScaledValueFromIntOrPercent(r.MaxUnavailableValue, totalCnt, true)
		if err != nil {
			return rejectAllWithErr(subjects, pass, rejects, "[%s] fail to get int value from raw max unavailable value(%s), error: %v", r.Name, r.MaxUnavailableValue.String(), err)
		}
		maxUnavailableQuota = quota
		allowUnavailable = quota
	}

	if r.MaxUnavailablePolicy != nil {
		quota, coeff, pow, err := getValueFromExponentiation(r.MaxUnavailablePolicy.ExpFunc, totalCnt, true)
		if err != nil {
			return rejectAllWithErr(subjects, pass, rejects, "[%s] fail to get max unavailable value with ExpFunc(coeff: %f, pow: %f, total: %d), error: %s", r.Name, coeff, pow, totalCnt, err)
		}
		if quota < maxUnavailableQuota {
			maxUnavailableQuota = quota
			allowUnavailable = quota
		}
	}

	if r.MinAvailableValue != nil {
		quota, err := intstr.GetScaledValueFromIntOrPercent(r.MinAvailableValue, totalCnt, false)
		if err != nil {
			return rejectAllWithErr(subjects, pass, rejects, "[%s] fail to get int value from raw min available value(%s), error: %v", r.Name, r.MaxUnavailableValue.String(), err)
		}
		minAvailableQuota = quota
	}

	if r.MinAvailablePolicy != nil {
		quota, coeff, pow, err := getValueFromExponentiation(r.MinAvailablePolicy.ExpFunc, totalCnt, false)
		if err != nil {
			return rejectAllWithErr(subjects, pass, rejects, "[%s] fail to get min unavailable value with ExpFunc(coeff: %f, pow: %f, total: %d), error: %s", r.Name, coeff, pow, totalCnt, err)
		}
		if quota > minAvailableQuota {
			minAvailableQuota = quota
		}
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
		rejects[podName] = fmt.Sprintf("blocked by min available policy: [min available]=%d/%d, [current keep available]=%d/%d", minAvailableQuota, totalCnt, allAvailableSize, totalCnt)
	}
	for podName := range rejectByMaxUnavailablePods {
		rejects[podName] = fmt.Sprintf("[%s] blocked by max unavailable policy: [max unavailable]=%d/%d, [current unavailable]=%d/%d", r.Name, maxUnavailableQuota, totalCnt, totalCnt-allAvailableSize, totalCnt)
	}

	if minTimeLeft != nil {
		interval := time.Duration(*minTimeLeft) * time.Second
		return &FilterResult{Passed: pass, Rejected: rejects, Interval: &interval, Err: fmt.Errorf("[%s] pods not finish warm up until %d seconds later", r.Name, minTimeLeft)}
	}

	return &FilterResult{Passed: pass, Rejected: rejects}
}

func (r *AvailableRuler) getPodReplicaSetReplication(controllerRef *metav1.OwnerReference, namespace string) (int, bool, error) {
	rs := &appsv1.ReplicaSet{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: controllerRef.Name}, rs); err != nil {
		return 0, false, fmt.Errorf("fail to find controller ReplicaSet %s/%s: %s", namespace, controllerRef.Name, err)
	}
	if rs.UID != controllerRef.UID {
		return 0, false, nil
	}

	if rs.Spec.Replicas == nil {
		return 0, true, nil
	}
	return int(*rs.Spec.Replicas), true, nil
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

func getValueFromExponentiation(exp *appsv1alpha1.ExpFunc, total int, roundUp bool) (int, float64, float64, error) {
	pow := 0.7
	coeff := 1.0
	var err error

	if exp.Pow != nil {
		pow, err = strconv.ParseFloat(*exp.Pow, 64)
		if err != nil {
			return 0, 0, 0, err
		}
	}

	if exp.Coeff != nil {
		coeff, err = strconv.ParseFloat(*exp.Coeff, 64)
		if err != nil {
			return 0, 0, 0, err
		}
	}

	if total == 0 {
		return 0, coeff, pow, nil
	}

	val := 0
	floatVal := coeff * math.Pow(float64(total), pow)
	if roundUp {
		val = int(math.Ceil(floatVal))
	} else {
		val = int(math.Floor(floatVal))
	}
	if val < 0 {
		return 0, coeff, pow, nil
	}

	if val > total {
		return total, coeff, pow, nil
	}

	return val, coeff, pow, nil
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
