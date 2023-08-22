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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kusionstack.io/kafed/pkg/controllers/ruleset/checker"
	"kusionstack.io/kafed/pkg/controllers/ruleset/register"
)

// ManagerInterface is ruleset manager interface to init and setup one ruleset controller
type ManagerInterface interface {

	// Register used to register ruleSet stages and conditions before starting controller
	register.Register

	// SetupRuleSetController add a new RuleSetController to manager
	SetupRuleSetController(manager.Manager) error

	// Check is used to check rule state after starting controller
	checker.Check
}

var defaultManager = newRulesetManager()

func RuleSetManager() ManagerInterface {
	return defaultManager
}

func RegisterListenChan(ctx context.Context) *source.Channel {
	ch := make(chan event.GenericEvent, 32)
	go func() {
		q := loadPodEventQueue()
		for {
			select {
			case <-ctx.Done():
				q.ShutDown()
				break
			default:
				item, shutdown := q.Get()
				if shutdown {
					break
				}
				tp, ok := item.(types.NamespacedName)
				if !ok {
					q.Done(item)
					continue
				}
				ch <- event.GenericEvent{Object: &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: tp.Namespace, Name: tp.Name}}}
				q.Done(item)
			}
		}
	}()
	return &source.Channel{Source: ch}
}

func AddUnAvailableFunc(f func(pod *corev1.Pod) (bool, *int64)) {
	register.UnAvailableFuncList = append(register.UnAvailableFuncList, f)
}

func newRulesetManager() ManagerInterface {
	return &rsManager{
		register: register.DefaultRegister(),
		checker:  checker.NewCheck(),
	}
}

func loadPodEventQueue() workqueue.DelayingInterface {
	podEventQueue := workqueue.NewDelayingQueue()
	podEventQueues = append(podEventQueues, podEventQueue)
	return podEventQueue
}

type rsManager struct {
	register   register.Register
	checker    checker.Check
	controller controller.Controller
}

func (m *rsManager) RegisterStage(key string, needCheck func(obj client.Object) bool) {
	m.register.RegisterStage(key, needCheck)
}

func (m *rsManager) RegisterCondition(opsCondition string, inCondition func(obj client.Object) bool) {
	m.register.RegisterCondition(opsCondition, inCondition)
}

func (m *rsManager) GetState(c client.Client, item client.Object) (checker.CheckState, error) {
	return m.checker.GetState(c, item)
}

func (m *rsManager) SetupRuleSetController(mgr manager.Manager) (err error) {
	m.controller, err = addToMgr(mgr, newReconciler(mgr))
	return err
}
