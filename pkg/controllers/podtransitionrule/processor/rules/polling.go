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
	"net/http"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	utilshttp "kusionstack.io/kuperator/pkg/utils/http"
)

const (
	MaxConcurrentTasks  = 16
	TaskDeadLineSeconds = 60
)

var (
	PollingManager = newPollingManager(context.TODO())
)

type PollingManagerInterface interface {
	Delete(id string)
	Add(id, url, caBundle, resourceKey string, timeout, interval time.Duration)
	GetResult(id string) *PollResult
	Start(ctx context.Context)
	AddListener(chan<- event.GenericEvent)
}

func newPollingManager(ctx context.Context) PollingManagerInterface {
	p := &pollingRunner{
		q:        workqueue.New(),
		tasks:    make(map[string]*task),
		ch:       make(chan struct{}, MaxConcurrentTasks),
		toDelete: make(chan string, 5),
	}
	go p.Start(ctx)
	return p
}

type pollingRunner struct {
	mu       sync.RWMutex
	tasks    map[string]*task
	q        workqueue.Interface
	toDelete chan string
	ch       chan struct{}

	listeners []chan<- event.GenericEvent
}

func (r *pollingRunner) AddListener(ch chan<- event.GenericEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, ch)
}

func (r *pollingRunner) Start(ctx context.Context) {
	stop := make(chan struct{})
	go r.worker(stop)
	<-ctx.Done()
	r.q.ShutDown()
	close(stop)
}

func (r *pollingRunner) worker(stop <-chan struct{}) {
	go func() {
		for {
			select {
			case id := <-r.toDelete:
				r.Delete(id)
			case <-stop:
				return
			}
		}
	}()
	for {
		id, shutdown := r.q.Get()
		if shutdown {
			return
		}
		select {
		case <-stop:
			return
		default:
		}
		r.acquire()
		taskId := id.(string)
		go r.doTask(taskId)
	}
}

func (r *pollingRunner) doTask(id string) {
	defer r.free()
	r.mu.RLock()
	t, ok := r.tasks[id]
	r.mu.RUnlock()
	if !ok || t == nil {
		r.q.Done(id)
		return
	}
	if time.Now().After(t.deadlineTime) {
		r.toDelete <- id
		r.q.Done(id)
		return
	}
	finish := t.do()
	r.broadcast(t.resourceKey)
	r.q.Done(id)
	if !finish {
		r.addAfter(id, t.interval)
	}
}

func (r *pollingRunner) broadcast(key string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	val := strings.Split(key, "/")
	for _, l := range r.listeners {
		l <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: val[0], Name: val[1]}}}
	}
}

func (r *pollingRunner) addAfter(id string, d time.Duration) {
	time.AfterFunc(d, func() {
		r.q.Add(id)
	})
}

func (r *pollingRunner) Add(id, url, caBundle, resourceKey string, timeout, interval time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tm := time.Now()
	t := &task{
		id:           id,
		url:          url,
		caBundle:     caBundle,
		resourceKey:  resourceKey,
		timeoutTime:  tm.Add(timeout),
		deadlineTime: tm.Add(timeout + (TaskDeadLineSeconds-1)*time.Second),
		interval:     interval,
		result: &PollResult{
			Approved:    sets.NewString(),
			LastMessage: "Waiting for first query...",
		},
	}
	r.tasks[id] = t
	r.addAfter(id, interval)
	r.addAfter(id, timeout+TaskDeadLineSeconds*time.Second)
}

func (r *pollingRunner) GetResult(id string) *PollResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tasks[id].getResult()
}

func (r *pollingRunner) Delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tasks, id)
}

func (r *pollingRunner) acquire() {
	r.ch <- struct{}{}
}

func (r *pollingRunner) free() {
	<-r.ch
}

type task struct {
	id          string
	url         string
	caBundle    string
	resourceKey string

	timeoutTime  time.Time
	deadlineTime time.Time
	interval     time.Duration

	latestQueryTime time.Time

	result *PollResult

	mu sync.RWMutex
}

func (t *task) do() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.result.Stopped {
		return false
	}
	if time.Now().After(t.timeoutTime) {
		t.result.Stopped = true
		t.result.LastMessage = fmt.Sprintf("Polling Timeout. %s", t.result.LastMessage)
		return false
	}
	t.latestQueryTime = time.Now()
	res, err := t.query()
	t.result.Count++
	t.result.LastError = err
	t.result.LastQueryTime = t.latestQueryTime
	if err != nil {
		return false
	}
	t.result.Approved.Insert(res.FinishedNames...)
	t.result.Info = t.info()
	t.result.LastMessage = res.Message
	if res.Success && res.Finished {
		t.result.ApproveAll = true
		t.result.Stopped = true
	}
	if res.Stop {
		t.result.Stopped = true
	}
	return t.result.Stopped
}

func (t *task) query() (*appsv1alpha1.PollResponse, error) {
	httpResp, err := utilshttp.DoHttpAndHttpsRequestWithCa(http.MethodGet, t.url, nil, nil, t.caBundle)
	defer func() {
		if httpResp != nil {
			_ = httpResp.Body.Close()
		}
	}()
	if err != nil {
		return nil, err
	}
	resp := &appsv1alpha1.PollResponse{}
	if err = utilshttp.ParseResponse(httpResp, resp); err != nil {
		return nil, err
	}

	return resp, nil
}

func (t *task) info() string {
	return fmt.Sprintf("task-id=%s, url=%s, latestQueryTime=%s.", t.id, t.url, t.latestQueryTime.String())
}

func (t *task) getResult() *PollResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result
}

type PollResult struct {
	Count         int
	Stopped       bool
	ApproveAll    bool
	Approved      sets.String
	LastMessage   string
	Info          string
	LastError     error
	LastQueryTime time.Time
}
