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
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

var (
	stage  = "PreTrafficOff"
	policy = appsv1alpha1.Fail
	rs     = &appsv1alpha1.PodTransitionRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "podtransitionrule-test",
			Namespace: "default",
		},
		Spec: appsv1alpha1.PodTransitionRuleSpec{
			Rules: []appsv1alpha1.TransitionRule{
				{
					Name:  "test-webhook",
					Stage: &stage,
					TransitionRuleDefinition: appsv1alpha1.TransitionRuleDefinition{
						Webhook: &appsv1alpha1.TransitionRuleWebhook{
							ClientConfig: appsv1alpha1.ClientConfig{
								URL: "http://127.0.0.1:8888",
							},
							FailurePolicy: &policy,
							Parameters: []appsv1alpha1.Parameter{
								{
									Key: "podIP",
									ValueFrom: &appsv1alpha1.ParameterSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "status.podIP",
										},
									},
								},
								{
									Key: "context",
									ValueFrom: &appsv1alpha1.ParameterSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.annotations['test.io/context']",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
)

func TestWebhookCase1(t *testing.T) {
	stop, finish := RunHttpServer(handleHttpAlwaysSuccess)
	defer func() {
		stop <- struct{}{}
		<-finish
	}()
	targets := map[string]*corev1.Pod{
		"test-pod-a": (&podTemplate{Name: "test-pod-a", Ip: "1.1.1.58"}).GetPod(),
		"test-pod-b": (&podTemplate{Name: "test-pod-b", Ip: "1.1.1.59"}).GetPod(),
		"test-pod-c": (&podTemplate{Name: "test-pod-c", Ip: "1.1.1.60"}).GetPod(),
	}
	subjects := sets.NewString("test-pod-a", "test-pod-b")
	g := gomega.NewGomegaWithT(t)
	webhooks := GetWebhook(rs)
	g.Expect(len(webhooks)).Should(gomega.BeEquivalentTo(1))
	web := webhooks[0]
	// 2 pass
	res := web.Do(targets, subjects)
	rj, _ := json.Marshal(res)
	fmt.Printf("res: %s", string(rj))
	g.Expect(res.Passed.Len()).Should(gomega.BeEquivalentTo(2))
}

func TestWebhookCase2(t *testing.T) {
	stop, finish := RunHttpServer(handleHttpAlwaysFalse)
	defer func() {
		stop <- struct{}{}
		<-finish
	}()
	targets := map[string]*corev1.Pod{
		"test-pod-a": (&podTemplate{Name: "test-pod-a", Ip: "1.1.1.58"}).GetPod(),
		"test-pod-b": (&podTemplate{Name: "test-pod-b", Ip: "1.1.1.59"}).GetPod(),
		"test-pod-c": (&podTemplate{Name: "test-pod-c", Ip: "1.1.1.60"}).GetPod(),
	}
	subjects := sets.NewString("test-pod-a")
	g := gomega.NewGomegaWithT(t)
	webhooks := GetWebhook(rs)
	g.Expect(len(webhooks)).Should(gomega.BeEquivalentTo(1))
	web := webhooks[0]
	res := web.Do(targets, subjects)
	rj, _ := json.Marshal(res)
	fmt.Printf("res: %s", string(rj))
	g.Expect(res.Passed.Len()).Should(gomega.BeEquivalentTo(0))
	g.Expect(len(res.Rejected)).Should(gomega.BeEquivalentTo(1))
}

func TestWebhookCase3(t *testing.T) {
	stop, finish := RunHttpServer(handleHttpWithTrace)
	defer func() {
		stop <- struct{}{}
		<-finish
	}()
	targets := map[string]*corev1.Pod{
		"test-pod-a": (&podTemplate{Name: "test-pod-a", Ip: "1.1.1.58"}).GetPod(),
		"test-pod-b": (&podTemplate{Name: "test-pod-b", Ip: "1.1.1.59"}).GetPod(),
		"test-pod-c": (&podTemplate{Name: "test-pod-c", Ip: "1.1.1.60"}).GetPod(),
	}
	subjects := sets.NewString("test-pod-a", "test-pod-b", "test-pod-c")
	g := gomega.NewGomegaWithT(t)
	webhooks := GetWebhook(rs)
	g.Expect(len(webhooks)).Should(gomega.BeEquivalentTo(1))
	web := webhooks[0]
	res := web.Do(targets, subjects)
	rj, _ := json.Marshal(res)
	fmt.Printf("res: %s\n", string(rj))
	g.Expect(res.Passed.Len()).Should(gomega.BeEquivalentTo(0))
	g.Expect(len(res.Rejected)).Should(gomega.BeEquivalentTo(3))
	g.Expect(res.RuleState).ShouldNot(gomega.BeNil())

	sts := &appsv1alpha1.RuleState{Name: web.RuleName, WebhookStatus: res.RuleState.WebhookStatus}
	rs.Status.RuleStates = []*appsv1alpha1.RuleState{sts}

	webhooks = GetWebhook(rs)
	web = webhooks[0]
	res = web.Do(targets, subjects)
	rj, _ = json.Marshal(res)
	fmt.Printf("res: %s\n", string(rj))

	// server interval = 4, client interval = 5
	// reject by interval
	g.Expect(res.Passed.Len()).Should(gomega.BeEquivalentTo(0))
	g.Expect(len(res.Rejected)).Should(gomega.BeEquivalentTo(3))

	<-time.After(5 * time.Second)
	sts = &appsv1alpha1.RuleState{Name: web.RuleName, WebhookStatus: res.RuleState.WebhookStatus}
	rs.Status.RuleStates = []*appsv1alpha1.RuleState{sts}
	webhooks = GetWebhook(rs)
	web = webhooks[0]
	res = web.Do(targets, subjects)
	rj, _ = json.Marshal(res)
	fmt.Printf("res: %s\n", string(rj))

	// server interval = 4, client interval = 5
	// reject by interval
	g.Expect(res.Passed.Len()).Should(gomega.BeEquivalentTo(2))
	g.Expect(len(res.Rejected)).Should(gomega.BeEquivalentTo(1))
}

type podTemplate struct {
	Name  string
	Ip    string
	Phase corev1.PodPhase
}

func (p *podTemplate) setDefault() {
	if p.Name == "" {
		p.Name = "default"
	}
	if p.Ip == "" {
		p.Ip = "127.0.0.1"
	}
	if p.Phase == "" {
		p.Phase = corev1.PodRunning
	}
}

func (p *podTemplate) GetPod() *corev1.Pod {
	p.setDefault()
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: "default",
			Labels: map[string]string{
				"app": "test-app",
			},
			Annotations: map[string]string{
				"test.io/context": "test-context",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "nginxImage",
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP: p.Ip,
			Phase: p.Phase,
		},
	}
}
