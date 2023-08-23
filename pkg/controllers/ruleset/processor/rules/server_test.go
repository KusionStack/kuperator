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

func RunHttpServer(f func(http.ResponseWriter, *http.Request)) (chan struct{}, chan struct{}) {
	fmt.Println("try run http server")
	defer fmt.Println("running")
	server := &Server{
		server: &http.Server{
			Addr:    "127.0.0.1:8888",
			Handler: http.HandlerFunc(f),
		},
	}
	stopped := make(chan struct{})
	finish := server.Run(stopped)
	return stopped, finish
}

func handleHttpAlwaysSuccess(resp http.ResponseWriter, req *http.Request) {
	fmt.Println(req.URL)
	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}
	webReq := &WebhookReq{}
	fmt.Printf("handle http req: %s", string(all))
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	webhookResp := &Response{
		Success: true,
		Message: "test success",
	}
	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func handleHttpAlwaysFalse(resp http.ResponseWriter, req *http.Request) {
	fmt.Println(req.URL)
	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}
	webReq := &WebhookReq{}
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	webhookResp := &Response{
		Success: false,
		Message: "test false",
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
	if cnt > s.pods.Len() {
		cnt = s.pods.Len()
		isAll = true
	}
	return sets.NewString(s.pods.List()[0:cnt]...), isAll
}

func handleHttpWithTrace(resp http.ResponseWriter, req *http.Request) {
	fmt.Println(req.URL)

	all, err := io.ReadAll(req.Body)
	if err != nil {
		fmt.Println(fmt.Sprintf("read body err: %s", err))
	}

	webReq := &WebhookReq{}
	err = json.Unmarshal(all, webReq)
	if err != nil {
		fmt.Printf("fail to unmarshal webhook request: %v", err)
		http.Error(resp, fmt.Sprintf("fail to unmarshal webhook request %s", string(all)), http.StatusInternalServerError)
		return
	}
	webhookResp := &Response{}
	pods := getPods(webReq)
	if webReq.RetryByTrace {
		col, ok := traceCache[webReq.TraceId]
		if !ok {
			panic(fmt.Sprintf("no trace %s", webReq.TraceId))
		}
		passed, ok := col.getNow()
		if ok {
			webhookResp = &Response{
				Success: true,
				Message: "success",
			}
		} else {
			webhookResp = &Response{
				Success:      false,
				Message:      fmt.Sprintf("server: success size %d", passed.Len()),
				Passed:       passed.List(),
				RetryByTrace: true,
			}
		}
	} else {
		col, ok := traceCache[webReq.TraceId]
		if !ok {
			col = newSetsTimer(pods)
			traceCache[webReq.TraceId] = col
		}
		webhookResp = &Response{
			Success:      false,
			Message:      fmt.Sprintf("server: retry by trace %s", webReq.TraceId),
			RetryByTrace: true,
		}
	}

	byt, _ := json.Marshal(webhookResp)
	resp.Write(byt)
}

func getPods(req *WebhookReq) sets.String {
	res := sets.NewString()
	for _, param := range req.Resources {
		res.Insert(param.Name)
	}
	return res
}
