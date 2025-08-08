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
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	controllerutils "kusionstack.io/kuperator/pkg/controllers/podtransitionrule/utils"
	"kusionstack.io/kuperator/pkg/utils"
	utilshttp "kusionstack.io/kuperator/pkg/utils/http"
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

	retryInterval *time.Duration
	taskInfo      map[string]*appsv1alpha1.TaskInfo
}

func (w *Webhook) Do(targets map[string]*corev1.Pod, subjects sets.String) *FilterResult {
	w.taskInfo = map[string]*appsv1alpha1.TaskInfo{}
	effectiveSubjects := sets.NewString(subjects.List()...)
	checked := sets.NewString()
	rejectedPods := map[string]string{}
	historyTaskInfo := map[string]*appsv1alpha1.TaskInfo{}
	for sub := range subjects {
		if w.Approved(targets[sub].Name) {
			effectiveSubjects.Delete(sub)
			checked.Insert(sub)
		}
	}

	newWebhookState := &appsv1alpha1.WebhookStatus{
		TaskStates: []appsv1alpha1.TaskInfo{},
	}
	defer func() {
		newWebhookState.TaskStates = w.convTaskInfo(w.taskInfo)
		newWebhookState.History = w.convTaskInfo(historyTaskInfo)
		w.State.WebhookStatus = newWebhookState
	}()
	allTracingPods := sets.NewString()
	nowTime := time.Now()
	for i, state := range w.State.WebhookStatus.History {
		if state.LastTime != nil && nowTime.Sub(state.LastTime.Time) < 10*time.Minute {
			historyTaskInfo[state.TaskId] = w.State.WebhookStatus.History[i].DeepCopy()
		}
	}

	for i, state := range w.State.WebhookStatus.TaskStates {
		if len(state.Processing) == 0 || !effectiveSubjects.HasAny(state.Processing...) {
			// invalid, move in history
			historyTaskInfo[state.TaskId] = &w.State.WebhookStatus.TaskStates[i]
			PollingManager.Delete(state.TaskId)
			continue
		}
		currentPods := sets.NewString(Intersection(effectiveSubjects, state.Processing)...)
		checked.Insert(Intersection(effectiveSubjects, state.Approved)...)
		allTracingPods.Insert(currentPods.List()...)
		taskId := state.TaskId

		// get latest polling result
		pollingResult := PollingManager.GetResult(taskId)

		// restart case
		if pollingResult == nil {
			pollUrl, _ := w.getPollingUrl(taskId)
			PollingManager.Add(
				taskId,
				pollUrl,
				w.Webhook.ClientConfig.Poll.CABundle,
				w.Key,
				time.Duration(*w.Webhook.ClientConfig.Poll.TimeoutSeconds)*time.Second,
				time.Duration(*w.Webhook.ClientConfig.Poll.IntervalSeconds)*time.Second,
			)
			rejectMsg := fmt.Sprintf(
				"Task %s polling result not found, try polling again, %s",
				w.Key,
				taskId,
			)
			for po := range currentPods {
				rejectedPods[po] = rejectMsg
			}
			continue
		}

		if pollingResult.ApproveAll {
			klog.Infof("polling task finished, approve all pods after %d times, %s, %s", pollingResult.Count, pollingResult.Info, pollingResult.LastMessage)
			w.recordTaskInfo(&state, pollingResult.LastMessage, pollingResult.LastQueryTime, state.Processing)
			checked.Insert(currentPods.List()...)
			PollingManager.Delete(taskId)
			continue
		}
		var errMsg string
		if pollingResult.LastError != nil {
			errMsg = fmt.Sprintf("polling task %s error, %v", taskId, pollingResult.LastError)
			klog.Warningf(errMsg)
		}
		var rejectMsg string
		if pollingResult.Stopped {
			// stopped, move in history
			newState := approve(state.DeepCopy(), pollingResult.Approved.List())
			newState.LastTime = &metav1.Time{Time: pollingResult.LastQueryTime}
			historyTaskInfo[taskId] = newState
			PollingManager.Delete(taskId)
			klog.Infof("polling task stopped after %d times, approved pods %v, %s, %s", pollingResult.Count, pollingResult.Approved.List(), pollingResult.Info, pollingResult.LastMessage)
			rejectMsg = fmt.Sprintf(
				"Not approved by webhook %s, polling task %s stopped %s %s",
				w.Key,
				taskId,
				pollingResult.LastMessage,
				errMsg,
			)
		} else {
			w.recordTaskInfo(&state, pollingResult.LastMessage, pollingResult.LastQueryTime, pollingResult.Approved.List())
			klog.Infof("polling task is running, current %d times, approved pods %v, %s, %s", pollingResult.Count, pollingResult.Approved.List(), pollingResult.Info, pollingResult.LastMessage)
			rejectMsg = fmt.Sprintf(
				"Not approved by webhook %s, polling task %s is running %s %s",
				w.Key,
				taskId,
				pollingResult.LastMessage,
				errMsg,
			)
		}
		for po := range currentPods {
			if pollingResult.Approved.Has(po) {
				checked.Insert(po)
			} else {
				if pollingResult.Stopped {
					allTracingPods.Delete(po)
				}
				rejectedPods[po] = rejectMsg
			}
		}
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
	selfTraceId, res, err := w.query(effectiveSubjects, targets)
	if err != nil {
		for eft := range effectiveSubjects {
			rejectedPods[eft] = fmt.Sprintf(
				"Fail to do webhook request %s, %v, traceId %s",
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

	localFinished := sets.NewString(res.FinishedNames...)
	// Prevent result tampering
	processing := effectiveSubjects.Difference(localFinished).List()
	approved := Intersection(effectiveSubjects, res.FinishedNames)
	if !res.Success {
		checked.Insert(approved...)
		for _, po := range processing {
			rejectedPods[po] = fmt.Sprintf(
				"Webhook check %s rejected, traceId %s, taskId %s, msg: %s",
				w.Key,
				selfTraceId,
				taskId,
				res.Message,
			)
		}
		// requeue
		w.updateInterval(defaultInterval)
	} else if !shouldPoll(res) {
		// success, All passed
		checked.Insert(effectiveSubjects.List()...)
	} else {
		// success, init poll task
		// trigger reconcile by PollingManager listener
		if taskId == "" {
			klog.Warningf("%s handle invalid webhook response, empty taskId in polling response", w.Key)
			for eft := range effectiveSubjects {
				rejectedPods[eft] = fmt.Sprintf("Invalid empty taskID, request trace %s", selfTraceId)
			}
			return &FilterResult{
				Passed:    checked,
				Rejected:  rejectedPods,
				Err:       err,
				RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
			}
		}

		pollUrl, err := w.getPollingUrl(taskId)
		if err != nil {
			for eft := range effectiveSubjects {
				rejectedPods[eft] = fmt.Sprintf("Fail to get %s polling config , %v", w.Key, err)
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
		w.newTaskInfo(taskId, res.Message, processing, approved)
		checked.Insert(approved...)
		for _, po := range processing {
			rejectedPods[po] = fmt.Sprintf(
				"Polling task %s initialized, will polling by taskId %s, msg: %s",
				w.Key,
				taskId,
				res.Message,
			)
		}
	}

	return &FilterResult{
		Passed:    checked,
		Rejected:  rejectedPods,
		Interval:  w.retryInterval,
		RuleState: &appsv1alpha1.RuleState{Name: w.RuleName, WebhookStatus: newWebhookState},
	}
}

func (w *Webhook) convTaskInfo(infoMap map[string]*appsv1alpha1.TaskInfo) []appsv1alpha1.TaskInfo {
	states := make([]appsv1alpha1.TaskInfo, 0, len(infoMap))
	for _, v := range infoMap {
		states = append(states, *v)
	}
	return states
}

func (w *Webhook) newTaskInfo(taskId, msg string, processing, approved []string) {
	w.taskInfo[taskId] = &appsv1alpha1.TaskInfo{
		BeginTime:  &metav1.Time{Time: time.Now()},
		Message:    msg,
		TaskId:     taskId,
		Processing: processing,
		Approved:   approved,
	}
}

func (w *Webhook) recordTaskInfo(old *appsv1alpha1.TaskInfo, msg string, updateTime time.Time, approved []string) {
	approvedSets := sets.NewString(old.Approved...)
	processingSets := sets.NewString(old.Processing...)
	for _, po := range approved {
		if processingSets.Has(po) {
			approvedSets.Insert(po)
		}
	}
	processingSets.Delete(approved...)
	var beginTime, lastTime *metav1.Time
	beginTime = old.BeginTime.DeepCopy()
	if updateTime.After(beginTime.Time) {
		lastTime = &metav1.Time{Time: updateTime}
	}
	w.taskInfo[old.TaskId] = &appsv1alpha1.TaskInfo{
		BeginTime:  beginTime,
		LastTime:   lastTime,
		Message:    msg,
		TaskId:     old.TaskId,
		Approved:   approvedSets.List(),
		Processing: processingSets.List(),
	}
}

func (w *Webhook) updateInterval(interval time.Duration) {
	if interval >= 0 && w.retryInterval == nil || *w.retryInterval > interval {
		w.retryInterval = &interval
	}
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

func (w *Webhook) query(podSet sets.String, targets map[string]*corev1.Pod) (string, *appsv1alpha1.WebhookResponse, error) {
	req, err := w.buildRequest(podSet, targets)
	if err != nil {
		return req.TraceId, nil, err
	}
	res, err := w.doHttp(req)
	return req.TraceId, res, err
}

func (w *Webhook) doHttp(req *appsv1alpha1.WebhookRequest) (*appsv1alpha1.WebhookResponse, error) {
	httpResp, err := utilshttp.DoHttpAndHttpsRequestWithCa(http.MethodPost, w.Webhook.ClientConfig.URL, *req, nil, w.Webhook.ClientConfig.CABundle)
	defer func() {
		_ = httpResp.Body.Close()
	}()
	if err != nil {
		return nil, err
	}
	resp := &appsv1alpha1.WebhookResponse{}
	if err = utilshttp.ParseResponse(httpResp, resp); err != nil {
		return nil, err
	}
	return resp, nil
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

func approve(taskInfo *appsv1alpha1.TaskInfo, approved []string) *appsv1alpha1.TaskInfo {
	approvedSets := sets.NewString(taskInfo.Approved...)
	processingSets := sets.NewString(taskInfo.Processing...)
	inProcessing := Intersection(processingSets, approved)
	processingSets.Delete(inProcessing...)
	approvedSets.Insert(inProcessing...)
	taskInfo.Approved = approvedSets.List()
	taskInfo.Processing = approvedSets.List()
	return taskInfo
}

func Intersection(s sets.String, t []string) (res []string) {
	for _, item := range t {
		if s.Has(item) {
			res = append(res, item)
		}
	}
	return res
}
