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
	"k8s.io/klog"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	controllerutils "kusionstack.io/operating/pkg/controllers/podtransitionrule/utils"
	"kusionstack.io/operating/pkg/utils"
	utilshttp "kusionstack.io/operating/pkg/utils/http"
)

type WebhookRuler struct {
	Name string
}

func (r *WebhookRuler) Filter(
	podTransitionRule *appsv1alpha1.PodTransitionRule,
	targets map[string]*corev1.Pod,
	subjects sets.String,
) *FilterResult {
	return GetWebhook(podTransitionRule, r.Name)[0].Do(targets, subjects)
}

const (
	defaultInterval = 5 * time.Second
)

func GetWebhook(pt *appsv1alpha1.PodTransitionRule, names ...string) (webs []*Webhook) {
	ruleNames := sets.NewString(names...)
	for i, rule := range pt.Spec.Rules {
		var web *appsv1alpha1.TransitionRuleWebhook
		if rule.Webhook == nil {
			continue
		}
		if ruleNames.Len() > 0 && !ruleNames.Has(rule.Name) {
			continue
		}
		web = pt.Spec.Rules[i].Webhook
		ruleState := &appsv1alpha1.RuleState{
			Name: rule.Name,
		}
		for j, state := range pt.Status.RuleStates {
			if state.Name == rule.Name {
				ruleState = pt.Status.RuleStates[j].DeepCopy()
				break
			}
		}
		if ruleState.WebhookStatus == nil {
			ruleState.WebhookStatus = &appsv1alpha1.WebhookStatus{}
		}
		if ruleState.WebhookStatus.ItemStatus == nil {
			ruleState.WebhookStatus.ItemStatus = []*appsv1alpha1.ItemStatus{}
		}
		if ruleState.WebhookStatus.TaskStates == nil {
			ruleState.WebhookStatus.TaskStates = []appsv1alpha1.TaskInfo{}
		}

		webs = append(webs, &Webhook{
			Stage:    rule.Stage,
			RuleName: rule.Name,
			Key:      pt.Namespace + "/" + pt.Name + "/" + rule.Name,
			Webhook:  web,
			State:    ruleState,
			Approved: func(po string) bool {
				return controllerutils.IsPodPassRule(po, pt, rule.Name)
			},
		})
	}
	return webs
}

type Webhook struct {
	Key      string
	RuleName string
	Stage    *string

	Webhook *appsv1alpha1.TransitionRuleWebhook
	State   *appsv1alpha1.RuleState

	Approved func(string) bool

	targets  map[string]*corev1.Pod
	subjects sets.String

	retryInterval *time.Duration
	taskInfo      map[string]*appsv1alpha1.TaskInfo
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
	w.taskInfo = map[string]*appsv1alpha1.TaskInfo{}
	effectiveSubjects := sets.NewString(w.subjects.List()...)
	checked := sets.NewString()
	rejectedPods := map[string]string{}

	for sub := range w.subjects {
		if w.Approved(targets[sub].Name) {
			effectiveSubjects.Delete(sub)
			checked.Insert(sub)
		}
	}

	newWebhookState := &appsv1alpha1.WebhookStatus{
		TaskStates: []appsv1alpha1.TaskInfo{},
		ItemStatus: []*appsv1alpha1.ItemStatus{},
	}
	defer func() {
		newWebhookState.TaskStates = w.convTaskInfo(w.taskInfo)
		w.State.WebhookStatus = newWebhookState
	}()

	taskPods := map[string]sets.String{}
	allTracingPods := sets.NewString()
	processingTask := sets.NewString()

	for _, state := range w.State.WebhookStatus.ItemStatus {
		if !effectiveSubjects.Has(state.Name) {
			continue
		}
		//processingPods.Insert(podState.Name)
		if taskPods[state.TaskId] == nil {
			taskPods[state.TaskId] = sets.NewString(state.Name)
		} else {
			taskPods[state.TaskId].Insert(state.Name)
		}

		if state.TaskId != "" {
			allTracingPods.Insert(state.Name)
		}

		if state.WebhookChecked {
			checked.Insert(state.Name)
		} else {
			processingTask.Insert(state.TaskId)
		}
	}

	// process trace
	for taskId := range processingTask {
		if taskId == "" {
			continue
		}
		pods := taskPods[taskId]
		pollingResult := PollingManager.GetResult(taskId)
		w.recordTask(taskId, pollingResult.LastMessage, pollingResult.LastQueryTime)
		if pollingResult.ApproveAll {
			klog.Infof("polling task finished, approve all pods after %d times, %s, %s", pollingResult.Count, pollingResult.Info, pollingResult.LastMessage)
			newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
				checked.Insert(po)
				return true
			}, taskId)
			PollingManager.Delete(taskId)
			continue
		}
		var errMsg string
		if pollingResult.LastError != nil {
			errMsg = fmt.Sprintf("polling task %s error, %v", taskId, pollingResult.LastError)
			klog.Warningf(errMsg)
		}
		if pollingResult.Stopped {
			klog.Infof("polling task stopped after %d times, approved pods %v, %s, %s", pollingResult.Count, pollingResult.Approved.List(), pollingResult.Info, pollingResult.LastMessage)
			for po := range pods {
				if pollingResult.Approved.Has(po) {
					checked.Insert(po)
				}
				if checked.Has(po) {
					newWebhookState.ItemStatus = append(newWebhookState.ItemStatus, &appsv1alpha1.ItemStatus{
						Name:           po,
						WebhookChecked: true,
						TaskId:         taskId,
					})
				} else {
					allTracingPods.Delete(po)
					rejectedPods[po] = fmt.Sprintf(
						"Not approved by webhook %s, polling task %s stoped %s %s",
						w.Key,
						taskId,
						pollingResult.LastMessage,
						errMsg,
					)
				}
			}
			PollingManager.Delete(taskId)
			continue
		}

		// always polling
		klog.Infof("polling task is running, current %d times, approved pods %v, %s, %s", pollingResult.Count, pollingResult.Approved.List(), pollingResult.Info, pollingResult.LastMessage)
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, pods, func(po string) bool {
			if pollingResult.Approved.Has(po) {
				checked.Insert(po)
			}
			hasChecked := checked.Has(po)
			if !hasChecked {
				rejectedPods[po] = fmt.Sprintf(
					"Not approved by webhook %s, polling task %s %s %s",
					w.Key,
					taskId,
					pollingResult.LastMessage,
					errMsg,
				)
			}
			return hasChecked
		}, taskId)
	}

	effectiveSubjects.Delete(allTracingPods.List()...)
	effectiveSubjects.Delete(checked.List()...)

	if effectiveSubjects.Len() == 0 {
		return &FilterResult{
			Passed:    checked,
			Rejected:  rejectedPods,
			Interval:  w.retryInterval,
			RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
		}
	}

	// First request
	selfTraceId, res, err := w.query(effectiveSubjects)
	if err != nil {
		for eft := range effectiveSubjects {
			rejectedPods[eft] = fmt.Sprintf(
				"fail to do webhook request %s, %v, traceId %s",
				w.Key,
				err,
				selfTraceId,
			)
		}
		klog.Errorf(
			"fail to request podtransitionrule webhook %s, pods: %v, traceId: %s, resp: %s",
			w.Key,
			effectiveSubjects.List(),
			selfTraceId,
			utils.DumpJSON(res),
		)
		return &FilterResult{
			Passed:    checked,
			Rejected:  rejectedPods,
			Err:       err,
			RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
		}
	}
	taskId := getTaskId(res)
	klog.Infof(
		"request podtransitionrule webhook %s, pods: %v, taskId: %s, traceId: %s, resp: %s",
		w.Key,
		effectiveSubjects.List(),
		taskId,
		selfTraceId,
		utils.DumpJSON(res),
	)
	if taskId != "" {
		w.recordTask(taskId, res.Message, time.Time{})
	}
	localFinished := sets.NewString(res.FinishedNames...)

	if !res.Success {
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, effectiveSubjects, func(po string) bool {
			if localFinished.Has(po) {
				checked.Insert(po)
			}
			hasChecked := checked.Has(po)
			if !hasChecked {
				rejectedPods[po] = fmt.Sprintf(
					"webhook check %s rejected, traceId %s, taskId %s, msg: %s",
					w.Key,
					selfTraceId,
					taskId,
					res.Message,
				)
			}
			return hasChecked
		}, "")
		w.updateInterval(defaultInterval)
	} else if !shouldPoll(res) {
		// success, All passed
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, effectiveSubjects, func(po string) bool {
			checked.Insert(po)
			return true
		}, taskId)
	} else {
		// success, poll
		pollUrl, err := w.getPollingUrl(taskId)
		if err != nil {
			for eft := range effectiveSubjects {
				rejectedPods[eft] = fmt.Sprintf("fail to get %s polling config , %v", w.Key, err)
			}
			return &FilterResult{
				Passed:    checked,
				Rejected:  rejectedPods,
				Err:       err,
				RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
			}
		}
		// add to polling manager
		PollingManager.Add(
			taskId,
			pollUrl,
			w.Webhook.ClientConfig.Poll.CABundle,
			w.Key,
			time.Duration(*w.Webhook.ClientConfig.Poll.TimeoutSeconds)*time.Second,
			time.Duration(*w.Webhook.ClientConfig.Poll.IntervalSeconds)*time.Second,
		)
		klog.Infof("%s, polling task %s initialized.", w.Key, taskId)
		newWebhookState.ItemStatus = appendStatus(newWebhookState.ItemStatus, effectiveSubjects, func(po string) bool {
			if localFinished.Has(po) {
				checked.Insert(po)
			}
			hasChecked := checked.Has(po)
			if !hasChecked {
				rejectedPods[po] = fmt.Sprintf(
					"polling task %s initialized, will polling by taskId %s, msg: %s",
					w.Key,
					taskId,
					res.Message,
				)
			}
			return hasChecked
		}, taskId)
	}

	return &FilterResult{
		Passed:    checked,
		Rejected:  rejectedPods,
		Interval:  w.retryInterval,
		RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
	}
}

func (w *Webhook) oldTraceMap() map[string]*appsv1alpha1.TaskInfo {
	res := map[string]*appsv1alpha1.TaskInfo{}
	for i, state := range w.State.WebhookStatus.TaskStates {
		res[state.TaskId] = &w.State.WebhookStatus.TaskStates[i]
	}
	return res
}

func (w *Webhook) convTaskInfo(infoMap map[string]*appsv1alpha1.TaskInfo) []appsv1alpha1.TaskInfo {
	states := make([]appsv1alpha1.TaskInfo, 0, len(infoMap))
	for _, v := range infoMap {
		states = append(states, *v)
	}
	return states
}

func (w *Webhook) recordTask(taskId string, msg string, updateTime time.Time) {
	tm := w.getTaskInfo(taskId)
	// first record
	var beginTime, lastTime *metav1.Time
	if tm == nil {
		beginTime = &metav1.Time{Time: time.Now()}
	} else {
		beginTime = tm.BeginTime.DeepCopy()
	}
	if updateTime.After(beginTime.Time) {
		lastTime = &metav1.Time{Time: updateTime}
	}
	w.taskInfo[taskId] = &appsv1alpha1.TaskInfo{
		BeginTime: beginTime,
		LastTime:  lastTime,
		Message:   msg,
		TaskId:    taskId,
	}
}

func (w *Webhook) getTaskInfo(traceId string) *appsv1alpha1.TaskInfo {
	if w.State.WebhookStatus == nil {
		return nil
	}
	for i, state := range w.State.WebhookStatus.TaskStates {
		if state.TaskId == traceId {
			return &w.State.WebhookStatus.TaskStates[i]
		}
	}
	return nil
}

func (w *Webhook) getPollingUrl(taskId string) (string, error) {
	if w.Webhook.ClientConfig.Poll == nil {
		return "", fmt.Errorf("null polling config in rule %s", w.Key)
	}
	pollUrl := fmt.Sprintf("%s?task-id=%s", w.Webhook.ClientConfig.Poll.URL, taskId)
	if w.Webhook.ClientConfig.Poll.RawQueryKey != "" {
		pollUrl = fmt.Sprintf("%s?%s=%s", w.Webhook.ClientConfig.Poll.URL, w.Webhook.ClientConfig.Poll.RawQueryKey, taskId)
	}
	return pollUrl, nil
}

func (w *Webhook) query(podSet sets.String) (string, *appsv1alpha1.WebhookResponse, error) {
	req, err := w.buildRequest(podSet)
	if err != nil {
		return req.TraceId, nil, err
	}
	res, err := w.doHttp(req)
	return req.TraceId, res, err
}

func (w *Webhook) doHttp(req *appsv1alpha1.WebhookRequest) (*appsv1alpha1.WebhookResponse, error) {
	httpResp, err := utilshttp.DoHttpAndHttpsRequestWithCa(http.MethodPost, w.Webhook.ClientConfig.URL, *req, nil, w.Webhook.ClientConfig.CABundle)
	if err != nil {
		return nil, err
	}
	resp := &appsv1alpha1.WebhookResponse{}
	if err = utilshttp.ParseResponse(httpResp, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func appendStatus(current []*appsv1alpha1.ItemStatus, nameSet sets.String, checkFunc func(string) bool, taskId string) []*appsv1alpha1.ItemStatus {
	for name := range nameSet {
		current = append(current, &appsv1alpha1.ItemStatus{
			Name:           name,
			WebhookChecked: checkFunc(name),
			TaskId:         taskId,
		})
	}
	return current
}

func shouldPoll(resp *appsv1alpha1.WebhookResponse) bool {
	return resp.Async || resp.Poll
}

func getTaskId(resp *appsv1alpha1.WebhookResponse) string {
	if resp != nil && (resp.Async || resp.Poll) {
		if resp.TaskId != "" {
			return resp.TaskId
		}
		return resp.TraceId
	}
	return ""
}
