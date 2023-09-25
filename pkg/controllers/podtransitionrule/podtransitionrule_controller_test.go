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
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	waitProcessFinished(request)
	podList := &corev1.PodList{}
	defer func() {
		c.List(ctx, podList, client.InNamespace("default"))
		for _, po := range podList.Items {
			g.Expect(c.Delete(ctx, &po)).NotTo(gomega.HaveOccurred())
		}
	}()

	g.Expect(c.List(ctx, podList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
	})).NotTo(gomega.HaveOccurred())

	// LifeCycle 1. set all pod on Stage
	for i := range podList.Items {
		if podList.Items[i].Name == "pod-test-1" || podList.Items[i].Name == "pod-test-2" {
			g.Expect(setPodUnavailable(&podList.Items[i])).NotTo(gomega.HaveOccurred())
		}
	}
	for i := range podList.Items {
		g.Expect(setPodOnStage(&podList.Items[i], PreTrafficOffStage)).NotTo(gomega.HaveOccurred())
	}
	// Ruleset Processing rules
	waitProcessFinished(request)
	g.Expect(c.List(ctx, podList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
	})).NotTo(gomega.HaveOccurred())
	// LifeCycle 2. 2 pod passed
	passedCount := 0
	for i := range podList.Items {
		state, err := PodTransitionRuleManager().GetState(c, &podList.Items[i])
		g.Expect(err).NotTo(gomega.HaveOccurred())
		printJson(state)
		printJson(podList.Items[i])
		if state.InStageAndPassed() {
			passedCount++
			g.Expect(podList.Items[i].Name).NotTo(gomega.Equal("pod-test-3"))
			g.Expect(podList.Items[i].Name).NotTo(gomega.Equal("pod-test-4"))
		}
	}
	waitProcessFinished(request)
	// 50% passed
	g.Expect(passedCount).Should(gomega.BeEquivalentTo(2))

	po := &corev1.Pod{}
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pod-test-3"}, po)).NotTo(gomega.HaveOccurred())
	g.Expect(setPodUnavailable(po)).NotTo(gomega.HaveOccurred())
	waitProcessFinished(request)
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pod-test-3"}, po)).NotTo(gomega.HaveOccurred())
	state, _ := PodTransitionRuleManager().GetState(c, po)
	g.Expect(state.InStageAndPassed()).Should(gomega.BeTrue())
	printJson(state)

	podTransitionRuleList := &appsv1alpha1.PodTransitionRuleList{}
	g.Expect(c.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-1")})).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-2")})).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-3")})).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-4")})).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(1))
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "pod-test-1"}, po)).NotTo(gomega.HaveOccurred())
	g.Expect(c.Delete(ctx, po)).NotTo(gomega.HaveOccurred())
	waitProcessFinished(request)
	g.Expect(c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "podtransitionrule-default"}, rs)).NotTo(gomega.HaveOccurred())
	printJson(rs)
	g.Expect(c.List(ctx, podTransitionRuleList, &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexPodTransitionRule, "pod-test-1")})).NotTo(gomega.HaveOccurred())
	g.Expect(len(podTransitionRuleList.Items)).Should(gomega.BeEquivalentTo(0))

	g.Expect(c.Delete(ctx, rs)).NotTo(gomega.HaveOccurred())
	waitProcessFinished(request)
	err := c.Get(ctx, types.NamespacedName{Namespace: "default", Name: "podtransitionrule-default"}, rs)
	g.Expect(errors.IsNotFound(err)).Should(gomega.BeTrue())
}

const (
	StageLabel       = "test.kafe.io/stage"
	ConditionLabel   = "test.kafe.io/condition"
	UnavailableLabel = "test.kafe.io/unavailable"

	PreTrafficOffStage = "PreTrafficOff"

	DeletePodCondition = "DeletePod"
)

func initRulesetManager() {

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

func waitProcessFinished(reqChan chan reconcile.Request) {
	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-time.After(5 * time.Second):
			return
		case <-timeout:
			fmt.Println("timeout!")
			return
		case <-reqChan:
			continue
		}
	}
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
