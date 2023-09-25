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
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	utilshttp "kusionstack.io/operating/pkg/utils/http"
)

type WebhookRuler struct {
	Name string
}

func (r *WebhookRuler) Filter(podTransitionRule *appsv1alpha1.PodTransitionRule, targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	return GetWebhook(podTransitionRule, r.Name)[0].Do(targets, subjects)
}

const (
	defaultTimeout  = 60 * time.Second
	defaultInterval = 5 * time.Second
)

func GetWebhook(rs *appsv1alpha1.PodTransitionRule, names ...string) (webs []*Webhook) {
	ruleNames := sets.NewString(names...)
	for i, rule := range rs.Spec.Rules {
		var web *appsv1alpha1.PodTransitionRuleRuleWebhook
		if rule.Webhook == nil {
			continue
		}
		if ruleNames.Len() > 0 && !ruleNames.Has(rule.Name) {
			continue
		}
		web = rs.Spec.Rules[i].Webhook
		ruleState := &appsv1alpha1.RuleState{
			Name: rule.Name,
		}
		for j, state := range rs.Status.RuleStates {
			if state.Name == rule.Name {
				ruleState = rs.Status.RuleStates[j].DeepCopy()
				break
			}
		}
		if ruleState.WebhookStatus == nil {
			ruleState.WebhookStatus = &appsv1alpha1.WebhookStatus{}
		}
		if ruleState.WebhookStatus.ItemStatus == nil {
			ruleState.WebhookStatus.ItemStatus = []*appsv1alpha1.ItemStatus{}
		}
		if ruleState.WebhookStatus.TraceStates == nil {
			ruleState.WebhookStatus.TraceStates = []appsv1alpha1.TraceInfo{}
		}

		webs = append(webs, &Webhook{
			Stage:    rule.Stage,
			RuleName: rule.Name,
			Key:      rs.Namespace + "/" + rs.Name + "/" + rule.Name,
			Webhook:  web,
			State:    ruleState,
		})
	}
	return webs
}

type Webhook struct {
	Key      string
	RuleName string
	Stage    *string

	Webhook *appsv1alpha1.PodTransitionRuleRuleWebhook
	State   *appsv1alpha1.RuleState

	targets  map[string]*corev1.Pod
	subjects sets.String

	retryInterval *time.Duration
	traceInfo     map[string]*appsv1alpha1.TraceInfo
}

func (w *Webhook) updateInterval(interval time.Duration) {
	if interval >= 0 && w.retryInterval == nil || *w.retryInterval > interval {
		w.retryInterval = &interval
	}
}

func (w *Webhook) setItems(targets map[string]*corev1.Pod, subjects sets.String) {
	w.targets = map[string]*corev1.Pod{}
	for k, v := range targets {
		w.targets[k] = v
	}
	w.subjects = sets.NewString(subjects.List()...)
}

func (w *Webhook) Do(targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	w.setItems(targets, subjects)
	w.traceInfo = map[string]*appsv1alpha1.TraceInfo{}
	effectiveSubjects := sets.NewString(w.subjects.List()...)
	passed := sets.NewString()
	rejectedPods := map[string]string{}

	for sub := range w.subjects {
		if alreadyApproved(targets[sub]) {
			effectiveSubjects.Delete(sub)
			passed.Insert(sub)
		}
	}

	newWebhookState := &appsv1alpha1.WebhookStatus{
		TraceStates: []appsv1alpha1.TraceInfo{},
		ItemStatus:  []*appsv1alpha1.ItemStatus{},
	}
	defer func() {
		newWebhookState.TraceStates = w.convTraceInfo(w.traceInfo)
		w.State.WebhookStatus = newWebhookState
	}()

	checked := sets.NewString()
	tracePods := map[string]sets.String{}
	allTracingPods := sets.NewString()
	processingTrace := sets.NewString()

	for _, state := range w.State.WebhookStatus.ItemStatus {
		if !effectiveSubjects.Has(state.Name) {
			continue
		}
		//processingPods.Insert(podState.Name)
		if tracePods[state.TraceId] == nil {
			tracePods[state.TraceId] = sets.NewString(state.Name)
		} else {
			tracePods[state.TraceId].Insert(state.Name)
		}

		if state.TraceId != "" {
			allTracingPods.Insert(state.Name)
		}

		if state.WebhookChecked {
			checked.Insert(state.Name)
		} else {
			processingTrace.Insert(state.TraceId)
		}
	}

	// process trace
	for traceId := range processingTrace {
		if traceId == "" {
			continue
		}
		pods := tracePods[traceId]

		// trace timeout, retrace
		if w.timeOut(traceId) {
			for po := range pods {
				if checked.Has(po) {
					newWebhookState.ItemStatus = append(newWebhookState.ItemStatus, &appsv1alpha1.ItemStatus{
						Name:           po,
						WebhookChecked: true,
						TraceId:        traceId,
					})
				} else {
					allTracingPods.Delete(po)
					rejectedPods[po] = fmt.Sprintf("webhook check [%s] timeout, trace %s", w.key(), traceId)
				}
			}
			continue
		}

		if ok, wait, cost := w.outInterval(traceId); !ok {
			var lastMsg string
			info := w.getTraceInfo(traceId)
			if info != nil {
				lastMsg = info.Message
			}
			newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
				hasChecked := checked.Has(po)
				if !hasChecked {
					rejectedPods[po] = fmt.Sprintf("webhook check [%s], traceId %s is waiting for next interval, msg: %s ,cost time %s", w.key(), traceId, lastMsg, cost.String())
				}
				return hasChecked
			}, traceId)
			w.recordTimeOld(traceId)
			w.updateInterval(wait / time.Second)
			continue
		}

		res, err := w.queryTrace(traceId)

		w.recordTime(traceId, res.Message)

		if err != nil {
			newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
				hasChecked := checked.Has(po)
				if !hasChecked {
					rejectedPods[po] = fmt.Sprintf("webhook check [%s] error, traceId %s, err: %v", w.key(), traceId, err)
				}
				return hasChecked
			}, traceId)
			continue
		}

		// all passed
		if res.Success {
			newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
				checked.Insert(po)
				return true
			}, traceId)
			continue
		}
		localFinished := sets.NewString(res.Passed...)
		// query trace
		if res.RetryByTrace {
			newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
				if localFinished.Has(po) {
					checked.Insert(po)
				}
				hasChecked := checked.Has(po)
				if !hasChecked {
					rejectedPods[po] = fmt.Sprintf("webhook check [%s] rejected, will retry by traceId %s, msg: %s", w.key(), traceId, res.Message)
				}
				return hasChecked
			}, traceId)
			continue
		}

		// finish trace
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
			if localFinished.Has(po) {
				checked.Insert(po)
			}
			hasChecked := checked.Has(po)
			if !hasChecked {
				rejectedPods[po] = fmt.Sprintf("webhook check [%s] rejected by finish trace, traceId %s, msg: %v", w.key(), traceId, res.Message)
			}
			return hasChecked
		}, "")
	}

	effectiveSubjects.Delete(allTracingPods.List()...)

	if effectiveSubjects.Len() == 0 {
		return &FilterResult{Passed: checked, Rejected: rejectedPods, Interval: w.retryInterval, RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState}}
	}

	// First request
	traceId, res, err := w.query(effectiveSubjects)

	if err != nil {
		for eft := range effectiveSubjects {
			rejectedPods[eft] = fmt.Sprintf("fail to do webhook [%s], %v, trace %s", w.key(), err, traceId)
		}
		return &FilterResult{Passed: checked, Rejected: rejectedPods, Err: err, RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState}}
	}
	w.recordTime(traceId, res.Message)
	localFinished := sets.NewString(res.Passed...)
	// All passed
	if res.Success {
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, effectiveSubjects, func(po string) bool {
			checked.Insert(po)
			return true
		}, traceId)
	} else if res.RetryByTrace {
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, effectiveSubjects, func(po string) bool {
			if localFinished.Has(po) {
				checked.Insert(po)
			}
			hasChecked := checked.Has(po)
			if !hasChecked {
				rejectedPods[po] = fmt.Sprintf("webhook check [%s] rejected, will retry by traceId %s, msg: %s", w.key(), traceId, res.Message)
			}
			return hasChecked
		}, traceId)
	} else {
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, effectiveSubjects, func(po string) bool {
			if localFinished.Has(po) {
				checked.Insert(po)
			}
			hasChecked := checked.Has(po)
			if !hasChecked {
				rejectedPods[po] = fmt.Sprintf("webhook check [%s] rejected, traceId %s, msg: %s", w.key(), traceId, res.Message)
			}
			return hasChecked
		}, "")
	}

	return &FilterResult{
		Passed:    checked,
		Rejected:  rejectedPods,
		Interval:  w.retryInterval,
		RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
	}
}

func (w *Webhook) oldTraceMap() map[string]*appsv1alpha1.TraceInfo {
	res := map[string]*appsv1alpha1.TraceInfo{}
	for i, state := range w.State.WebhookStatus.TraceStates {
		res[state.TraceId] = &w.State.WebhookStatus.TraceStates[i]
	}
	return res
}

func (w *Webhook) convTraceInfo(infoMap map[string]*appsv1alpha1.TraceInfo) []appsv1alpha1.TraceInfo {
	states := make([]appsv1alpha1.TraceInfo, 0, len(infoMap))
	for _, v := range infoMap {
		states = append(states, *v)
	}
	return states
}

func (w *Webhook) recordTime(traceId, msg string) {
	timeNow := time.Now()
	newRecord := &appsv1alpha1.TraceInfo{
		TraceId:   traceId,
		BeginTime: &metav1.Time{Time: timeNow},
		LastTime:  &metav1.Time{Time: timeNow},
		Message:   msg,
	}
	tm := w.getTraceInfo(traceId)
	if tm != nil && tm.BeginTime != nil {
		newRecord.BeginTime = tm.BeginTime.DeepCopy()
	}
	w.traceInfo[traceId] = newRecord
}

func (w *Webhook) recordTimeOld(traceId string) {
	tm := w.getTraceInfo(traceId)
	if tm != nil {
		w.traceInfo[traceId] = &appsv1alpha1.TraceInfo{
			BeginTime: tm.BeginTime.DeepCopy(),
			LastTime:  tm.LastTime.DeepCopy(),
			Message:   tm.Message,
			TraceId:   traceId,
		}
	}
}

func (w *Webhook) timeOut(traceId string) bool {
	timeNow := time.Now()
	timeOut := defaultTimeout
	tm := w.getTraceInfo(traceId)
	if tm == nil || tm.BeginTime == nil {
		return false
	}
	if w.Webhook.ClientConfig.TraceTimeoutSeconds != nil {
		timeOut = time.Duration(*w.Webhook.ClientConfig.TraceTimeoutSeconds) * time.Second
	}
	return timeNow.Sub(tm.BeginTime.Time) > timeOut
}

func (w *Webhook) getTraceInfo(traceId string) *appsv1alpha1.TraceInfo {
	if w.State.WebhookStatus == nil {
		return nil
	}
	for i, state := range w.State.WebhookStatus.TraceStates {
		if state.TraceId == traceId {
			return &w.State.WebhookStatus.TraceStates[i]
		}
	}
	return nil
}

func (w *Webhook) outInterval(traceId string) (bool, time.Duration, time.Duration) {
	tm := w.getTraceInfo(traceId)
	if tm == nil || tm.LastTime == nil {
		return true, 0, 0
	}
	interval := defaultInterval
	if w.Webhook.ClientConfig.IntervalSeconds != nil {
		interval = time.Duration(*w.Webhook.ClientConfig.IntervalSeconds) * time.Second
	}
	allCost := time.Since(tm.BeginTime.Time)
	nowInterval := time.Since(tm.LastTime.Time)
	wait := interval - nowInterval
	return nowInterval > interval, wait, allCost
}

func (w *Webhook) queryTrace(traceId string) (*Response, error) {
	req, err := w.buildRequest(sets.NewString(), &traceId)
	if err != nil {
		return nil, err
	}
	return w.doHttp(req)
}

func (w *Webhook) query(podSet sets.String) (string, *Response, error) {
	req, err := w.buildRequest(podSet, nil)
	if err != nil {
		return req.TraceId, nil, err
	}
	res, err := w.doHttp(req)
	return req.TraceId, res, err
}
func (w *Webhook) doHttp(req *WebhookReq) (*Response, error) {
	httpResp, err := utilshttp.DoHttpAndHttpsRequestWithCa(http.MethodPost, w.Webhook.ClientConfig.URL, *req, nil, w.Webhook.ClientConfig.CABundle)
	if err != nil {
		return nil, err
	}
	resp := &Response{}
	if err = utilshttp.ParseResponse(httpResp, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (w *Webhook) key() string {
	return w.Key
}

func appendStatus(current []*appsv1alpha1.ItemStatus, nameSet sets.String, checkFunc func(string) bool, trace string) []*appsv1alpha1.ItemStatus {
	for name := range nameSet {
		current = append(current, &appsv1alpha1.ItemStatus{
			Name:           name,
			WebhookChecked: checkFunc(name),
			TraceId:        trace,
		})
	}
	return current
}

func alreadyApproved(po *corev1.Pod) bool {
	return false
}
