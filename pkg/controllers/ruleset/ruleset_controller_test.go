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

package ruleset

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/ruleset/register"
)

func TestRuleset(t *testing.T) {
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
		g.Expect(c.Create(context.TODO(), po)).NotTo(gomega.HaveOccurred())
	}
	defer func() {
		for _, po := range pods {
			c.Delete(context.TODO(), po)
		}
	}()
	istr := intstr.FromString("50%")
	stage := PreTrafficOffStage
	rs := &appsv1alpha1.RuleSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ruleset-default",
			Namespace: "default",
		},
		Spec: appsv1alpha1.RuleSetSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test": "gen",
				},
			},
			Rules: []appsv1alpha1.RuleSetRule{
				{
					Stage: &stage,
					Name:  "serviceAvailable",
					RuleSetRuleDefinition: appsv1alpha1.RuleSetRuleDefinition{
						AvailablePolicy: &appsv1alpha1.AvailableRule{
							MaxUnavailableValue: &istr,
						},
					},
				},
			},
		},
	}
	g.Expect(c.Create(context.TODO(), rs)).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), rs)
	waitProcessFinished(request)
	podList := &corev1.PodList{}
	g.Expect(c.List(context.TODO(), podList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
	})).NotTo(gomega.HaveOccurred())

	// LifeCycle 1. set all pod on Stage
	for i := range podList.Items {
		g.Expect(setPodOnStage(&podList.Items[i], PreTrafficOffStage)).NotTo(gomega.HaveOccurred())
	}
	// Ruleset Processing rules
	waitProcessFinished(request)
	g.Expect(c.List(context.TODO(), podList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(rs.Spec.Selector.MatchLabels),
	})).NotTo(gomega.HaveOccurred())
	// LifeCycle 2. 2 pod passed
	passedCount := 0
	for i := range podList.Items {
		state, err := RuleSetManager().GetState(c, &podList.Items[i])
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if state.Passed && state.InStage(PreTrafficOffStage) {
			passedCount++
		}
		printJson(state)
	}
	// 50% passed
	g.Expect(passedCount).Should(gomega.BeEquivalentTo(2))
	<-time.After(5 * time.Second)
}

const (
	StageLabel       = "test.kafe.io/stage"
	ConditionLabel   = "test.kafe.io/condition"
	UnavailableLabel = "test.kafe.io/unavailable"

	PreTrafficOffStage = "PreTrafficOff"

	DeletePodCondition = "DeletePod"
)

func initRulesetManager() {

	register.UnAvailableFuncList = append(register.UnAvailableFuncList, func(pod *corev1.Pod) (bool, *int32) {
		if pod.GetLabels() == nil {
			return false, nil
		}
		_, ok := pod.GetLabels()[UnavailableLabel]
		return ok, nil
	})

	RuleSetManager().RegisterStage(PreTrafficOffStage, func(po client.Object) bool {
		if po.GetLabels() == nil {
			return false
		}
		val, ok := po.GetLabels()[StageLabel]
		return ok && val == PreTrafficOffStage
	})

	RuleSetManager().RegisterCondition(DeletePodCondition, func(po client.Object) bool {
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
		if err := c.Get(context.TODO(), types.NamespacedName{Namespace: po.Namespace, Name: po.Name}, po); err != nil {
			return err
		}
		po.Labels[StageLabel] = stage
		return c.Update(context.TODO(), po)
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
