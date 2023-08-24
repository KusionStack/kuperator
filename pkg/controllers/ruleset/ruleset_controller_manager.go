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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

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

func AddUnAvailableFunc(f func(pod *corev1.Pod) (bool, *int64)) {
	register.UnAvailableFuncList = append(register.UnAvailableFuncList, f)
}

func newRulesetManager() ManagerInterface {
	return &rsManager{
		register: register.DefaultRegister(),
		checker:  checker.NewCheck(),
	}
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
