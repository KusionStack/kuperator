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

package podtransitionrule

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule/register"
	"kusionstack.io/operating/pkg/utils/inject"
)

func TestPodTransitionRule(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	defer Stop()
	<-time.After(1 * time.Second)
	var pods []*corev1.Pod
	pods = append(pods,
		genDefaultPod("default", "pod-test-1"),
		genDefaultPod("default", "pod-test-2"),
		genDefaultPod("default", "pod-test-3"),
		genDefaultPod("default", "pod-test-4"))
	for _, po := range pods {
		g.Expect(c.Create(ctx, po)).NotTo(gomega.HaveOccurred())
	}
	istr := intstr.FromString("50%")
	stage := PreTrafficOffStage
	rs := &appsv1alpha1.PodTransitionRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "podtransitionrule-default",
			Namespace: "default",
		},
		Spec: appsv1alpha1.PodTransitionRuleSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test": "gen",
				},
			},
			Rules: []appsv1alpha1.TransitionRule{
				{
					Stage: &stage,
					Name:  "serviceAvailable",
					TransitionRuleDefinition: appsv1alpha1.TransitionRuleDefinition{
						AvailablePolicy: &appsv1alpha1.AvailableRule{
							MaxUnavailableValue: &istr,
						},
					},
				},
			},
		},
	}
	g.Expect(c.Create(ctx, rs)).NotTo(gomega.HaveOccurred())
	podList := &corev1.PodList{}
	defer func() {
		c.List(ctx, podList, client.InNamespace("default"))
		for _, po := range podList.Items {
			g.Expect(c.Delete(ctx, &po)).NotTo(gomega.HaveOccurred())
		}
	}()
	g.Eventually(func() int {
		c.List(ctx, podList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
		})
		return len(podList.Items)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(4))
	// LifeCycle 1. set all pod on Stage
	for i := range podList.Items {
		if podList.Items[i].Name == "pod-test-1" || podList.Items[i].Name == "pod-test-2" {
			g.Expect(setPodUnavailable(&podList.Items[i])).NotTo(gomega.HaveOccurred())
		}
	}
	for i := range podList.Items {
		g.Expect(setPodOnStage(&podList.Items[i], PreTrafficOffStage)).NotTo(gomega.HaveOccurred())
	}
	g.Eventually(func() int {
		passedCount := 0
		g.Expect(c.List(ctx, podList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
		})).NotTo(gomega.HaveOccurred())
		for i := range podList.Items {
			state, _ := PodTransitionRuleManager().GetState(ctx, c, &podList.Items[i])
			if state.InStageAndPassed() {
				passedCount++
			}
		}
		return passedCount
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(2))
	po := &corev1.Pod{}
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pod-test-3"}, po)).ShouldNot(gomega.HaveOccurred())
	g.Expect(setPodUnavailable(po)).ShouldNot(gomega.HaveOccurred())

	g.Eventually(func() bool {
		g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pod-test-3"}, po)).ShouldNot(gomega.HaveOccurred())
		state, _ := PodTransitionRuleManager().GetState(ctx, c, po)
		return state.InStageAndPassed()
	}, 5*time.Second, 1*time.Second).Should(gomega.BeTrue())
	podTransitionRuleList := &appsv1alpha1.PodTransitionRuleList{}
	g.Expect(c.List(
		ctx,
		podTransitionRuleList,
		&client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-1")},
	)).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.List(
		ctx,
		podTransitionRuleList,
		&client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-2")},
	)).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.List(
		ctx,
		podTransitionRuleList,
		&client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-3")},
	)).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.List(
		ctx,
		podTransitionRuleList,
		&client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-4")},
	)).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pod-test-1"}, po)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Delete(ctx, po)).NotTo(gomega.HaveOccurred())
	g.Eventually(func() int {
		g.Expect(c.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-1")})).NotTo(gomega.HaveOccurred())
		return len(podTransitionRuleList.Items)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(0))
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "podtransitionrule-default"}, rs)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Delete(ctx, rs)).NotTo(gomega.HaveOccurred())
	g.Eventually(func() error {
		err := c.Get(ctx, types.NamespacedName{
			Namespace: "default",
			Name:      "podtransitionrule-default",
		}, rs)
		if err != nil {
			fmt.Println(err.Error())
		}
		return err
	}, 5*time.Second, 1*time.Second).Should(gomega.HaveOccurred())
}

func TestWebhookRule(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	stop, finish := RunHttpServer(handleHttpAlwaysSuccess, "8888")
	defer func() {
		Stop()
		stop <- struct{}{}
		<-finish
	}()
	<-time.After(1 * time.Second)
	var pods []*corev1.Pod
	pods = append(pods,
		genDefaultPod("default", "pod-test-1"),
		genDefaultPod("default", "pod-test-2"),
		genDefaultPod("default", "pod-test-3"),
		genDefaultPod("default", "pod-test-4"))
	for _, po := range pods {
		g.Expect(c.Create(ctx, po)).NotTo(gomega.HaveOccurred())
	}
	stage := PreTrafficOffStage
	policy := appsv1alpha1.Fail
	rs := &appsv1alpha1.PodTransitionRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "podtransitionrule-webhook",
			Namespace: "default",
		},
		Spec: appsv1alpha1.PodTransitionRuleSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test": "gen",
				},
			},
			Rules: []appsv1alpha1.TransitionRule{
				{
					Stage: &stage,
					Name:  "webhook",
					TransitionRuleDefinition: appsv1alpha1.TransitionRuleDefinition{
						Webhook: &appsv1alpha1.TransitionRuleWebhook{
							ClientConfig: appsv1alpha1.ClientConfigBeta1{
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
	g.Expect(c.Create(ctx, rs)).NotTo(gomega.HaveOccurred())
	podList := &corev1.PodList{}
	defer func() {
		c.List(ctx, podList, client.InNamespace("default"))
		for _, po := range podList.Items {
			g.Expect(c.Delete(ctx, &po)).NotTo(gomega.HaveOccurred())
		}
	}()
	g.Eventually(func() int {
		g.Expect(c.List(ctx, podList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
		})).ShouldNot(gomega.HaveOccurred())
		return len(podList.Items)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(4))
	for i := range podList.Items {
		if podList.Items[i].Name == "pod-test-1" {
			g.Expect(setPodOnStage(&podList.Items[i], PreTrafficOffStage)).NotTo(gomega.HaveOccurred())
		}
	}
	g.Eventually(func() int {
		g.Expect(c.List(ctx, podList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
		})).NotTo(gomega.HaveOccurred())
		// LifeCycle 2. 2 pod passed
		passedCount := 0
		for i := range podList.Items {
			state, err := PodTransitionRuleManager().GetState(ctx, c, &podList.Items[i])
			g.Expect(err).NotTo(gomega.HaveOccurred())
			if state.InStageAndPassed() && state.Stage == PreTrafficOffStage {
				passedCount++
				g.Expect(podList.Items[i].Name).Should(gomega.Equal("pod-test-1"))
			}
		}
		return passedCount
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(1))

	g.Eventually(func() int {
		g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "podtransitionrule-webhook"}, rs)).Should(gomega.BeNil())
		return len(rs.Status.Details)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(1))

	// LifeCycle 1. set all pod on Stage
	for i := range podList.Items {
		g.Expect(setPodOnStage(&podList.Items[i], PreTrafficOffStage)).NotTo(gomega.HaveOccurred())
	}

	g.Eventually(func() int {
		g.Expect(c.List(ctx, podList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
		})).NotTo(gomega.HaveOccurred())
		passedCount := 0
		for i := range podList.Items {
			state, err := PodTransitionRuleManager().GetState(ctx, c, &podList.Items[i])
			g.Expect(err).NotTo(gomega.HaveOccurred())
			if state.InStageAndPassed() && state.Stage == PreTrafficOffStage {
				passedCount++
			}
		}
		return passedCount
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(4))

	g.Eventually(func() int {
		g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "podtransitionrule-webhook"}, rs)).Should(gomega.BeNil())
		return len(rs.Status.Details)
	}, 5*time.Second, 1*time.Second).Should(gomega.Equal(4))

	g.Expect(c.Delete(ctx, rs)).NotTo(gomega.HaveOccurred())
	g.Eventually(func() error {
		err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "podtransitionrule-webhook"}, rs)
		return err
	}, 5*time.Second, 1*time.Second).Should(gomega.HaveOccurred())
}

const (
	StageLabel       = "test.kafe.io/stage"
	ConditionLabel   = "test.kafe.io/condition"
	UnavailableLabel = "test.kafe.io/unavailable"

	PreTrafficOffStage = "PreTrafficOff"

	DeletePodCondition = "DeletePod"
)

func initPodTransitionRuleManager() {

	register.UnAvailableFuncList = []register.UnAvailableFunc{func(pod *corev1.Pod) (bool, *int64) {
		if pod.GetLabels() == nil {
			return false, nil
		}
		_, ok := pod.GetLabels()[UnavailableLabel]
		return ok, nil
	}}
	PodTransitionRuleManager().RegisterStage(PreTrafficOffStage, func(po client.Object) bool {
		if po.GetLabels() == nil {
			return false
		}
		val, ok := po.GetLabels()[StageLabel]
		return ok && val == PreTrafficOffStage
	})

	PodTransitionRuleManager().RegisterCondition(DeletePodCondition, func(po client.Object) bool {
		if po.GetLabels() == nil {
			return false
		}
		val, ok := po.GetLabels()[ConditionLabel]
		return ok && val == DeletePodCondition
	})
}

func setPodOnStage(po *corev1.Pod, stage string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, types.NamespacedName{Namespace: po.Namespace, Name: po.Name}, po); err != nil {
			return err
		}
		po.Labels[StageLabel] = stage
		return c.Update(ctx, po)
	})
}

func setPodUnavailable(po *corev1.Pod) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, types.NamespacedName{Namespace: po.Namespace, Name: po.Name}, po); err != nil {
			return err
		}
		po.Labels[UnavailableLabel] = "true"
		return c.Update(ctx, po)
	})
}

func genDefaultPod(namespace, name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"test": "gen",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "nginx",
					Image: "nginx:latest",
				},
			},
		},
	}
}

func printJson(obj any) {
	byt, _ := json.MarshalIndent(obj, "", "  ")
	fmt.Printf("%s\n", string(byt))
}

func handleHttpAlwaysSuccess(resp http.ResponseWriter, req *http.Request) {
	fmt.Println(req.URL)
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
