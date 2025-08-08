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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

type Server struct {
	server *http.Server
}

func (s *Server) Run(stop <-chan struct{}) chan struct{} {
	go func() {
		if err := s.server.ListenAndServe(); err != nil {
			fmt.Println(err)
		}
	}()

	<-time.After(5 * time.Second)
	finish := make(chan struct{})
	fmt.Println("run server")
	go func() {
		select {
		case <-stop:
			s.server.Close()
			finish <- struct{}{}
		}
	}()
	return finish
}

func RunHttpServer(f func(http.ResponseWriter, *http.Request), port string) (chan struct{}, chan struct{}) {
	fmt.Println("try run http server")
	defer fmt.Println("running")
	server := &Server{
		server: &http.Server{
			Addr:    fmt.Sprintf("127.0.0.1:%s", port),
			Handler: http.HandlerFunc(f),
		},
	}
	stopped := make(chan struct{})
	finish := server.Run(stopped)
	return stopped, finish
}

func handleHttpAlwaysSuccess(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleHttpAlwaysSuccess, %s\n", req.URL)
	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}
	webReq := &appsv1alpha1.WebhookRequest{}
	fmt.Printf("handle http req: %s", string(all))
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	webhookResp := &appsv1alpha1.WebhookResponse{
		Success: true,
		Message: "test success",
	}
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func handleHttpAlwaysFalse(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleHttpAlwaysFalse, %s\n", req.URL)
	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}
	webReq := &appsv1alpha1.WebhookRequest{}
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	webhookResp := &appsv1alpha1.WebhookResponse{
		Success: false,
		Message: "test false",
	}
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func handleHttpAlwaysSomeSucc(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleHttpAlwaysSomeSucc, %s\n", req.URL)
	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}
	webReq := &appsv1alpha1.WebhookRequest{}
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	pods := getPods(webReq)
	var local []string
	if pods.Len() > 0 {
		local = pods.List()[0 : pods.Len()-1]
	}
	webhookResp := &appsv1alpha1.WebhookResponse{
		Success:       false,
		Message:       "test finishedNames",
		FinishedNames: local,
	}
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

var (
	traceCache = map[string]*setsTimer{}
)

type setsTimer struct {
	pods     sets.String
	tm       time.Time
	interval time.Duration
}

func newSetsTimer(pods sets.String) *setsTimer {
	return &setsTimer{pods: pods, tm: time.Now(), interval: time.Second * 4}
}

func (s *setsTimer) getNow() (sets.String, bool) {
	isAll := false
	allTime := time.Now().Sub(s.tm)
	cnt := int(allTime/s.interval) + 1
	if cnt >= s.pods.Len() {
		cnt = s.pods.Len()
		isAll = true
	}
	return sets.NewString(s.pods.List()[0:cnt]...), isAll
}

func handleFirstPollSucc(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleFirstPollSucc, %s\n", req.URL)
	webReq := &appsv1alpha1.WebhookRequest{}
	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	// expect poll by trace id.
	webhookResp := &appsv1alpha1.WebhookResponse{
		Success: true,
		Async:   true,
		TraceId: NewTrace(),
	}
	pods := getPods(webReq)
	col := newSetsTimer(pods)
	traceCache[webhookResp.TraceId] = col
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func handleHttpWithTaskIdSucc(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleHttpWithTaskIdSucc, %s\n", req.URL)
	taskId := req.URL.Query().Get("trace-id")

	col, ok := traceCache[taskId]
	if !ok {
		panic(fmt.Sprintf("taskId %s not found", taskId))
	}
	passed, ok := col.getNow()
	var webhookResp *appsv1alpha1.PollResponse
	if ok {
		webhookResp = &appsv1alpha1.PollResponse{
			Success:  true,
			Message:  "success",
			Finished: true,
		}
	} else {
		webhookResp = &appsv1alpha1.PollResponse{
			Success:       true,
			Message:       fmt.Sprintf("server: success size %d", passed.Len()),
			Finished:      false,
			FinishedNames: passed.List(),
		}
	}
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func handleHttpWithTaskIdFail(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleHttpWithTaskIdFail, %s\n", req.URL)
	taskId := req.URL.Query().Get("trace-id")

	col, ok := traceCache[taskId]
	if !ok {
		panic(fmt.Sprintf("taskId %s not found", taskId))
	}
	passed, ok := col.getNow()
	var webhookResp *appsv1alpha1.PollResponse
	if ok {
		webhookResp = &appsv1alpha1.PollResponse{
			Success: false,
			Message: "fail",
			Stop:    true,
		}
	} else {
		webhookResp = &appsv1alpha1.PollResponse{
			Success:       true,
			Message:       fmt.Sprintf("server: success size %d", passed.Len()),
			Finished:      false,
			FinishedNames: passed.List(),
		}
	}
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func handleHttpError(resp http.ResponseWriter, req *http.Request) {
	fmt.Printf("handleHttpError, %s\n", req.URL)
	http.Error(resp, fmt.Sprintf("test http error case"), http.StatusInternalServerError)
}

func getPods(req *appsv1alpha1.WebhookRequest) sets.String {
	res := sets.NewString()
	for _, param := range req.Resources {
		res.Insert(param.Name)
	}
	return res
}
