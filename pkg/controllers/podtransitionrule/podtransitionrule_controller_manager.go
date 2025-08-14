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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/checker"
	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/register"
)

// ManagerInterface is podtransitionrule manager interface to init and setup one podtransitionrule controller
type ManagerInterface interface {
	// Register used to register podTransitionRule stages and conditions before starting controller
	register.Register

	// Checker is used to check rule state after starting controller
	checker.Checker

	// SetupPodTransitionRuleController add a new PodTransitionRuleController to manager
	SetupPodTransitionRuleController(manager.Manager) error
}

var defaultManager = newPodTransitionRuleManager()

func PodTransitionRuleManager() ManagerInterface {
	return defaultManager
}

func AddUnAvailableFunc(f func(pod *corev1.Pod) (bool, *int64)) {
	register.UnAvailableFuncList = append(register.UnAvailableFuncList, f)
}

func newPodTransitionRuleManager() ManagerInterface {
	return &rsManager{
		Register: register.DefaultRegister(),
		Checker:  checker.NewCheck(),
	}
}

type rsManager struct {
	register.Register
	checker.Checker
	controller controller.Controller
}

func (m *rsManager) SetupPodTransitionRuleController(mgr manager.Manager) (err error) {
	m.controller, err = addToMgr(mgr, newReconciler(mgr))
	return err
}
