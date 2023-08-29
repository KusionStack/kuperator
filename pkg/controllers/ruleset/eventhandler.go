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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	commonutils "kusionstack.io/kafed/pkg/utils"
)

var _ inject.Client = &EventHandler{}
var _ inject.Logger = &EventHandler{}

type EventHandler struct {
	// client and logger will be injected
	client client.Client
	logger logr.Logger
}

func (p *EventHandler) InjectClient(c client.Client) error {
	p.client = c
	return nil
}

func (p *EventHandler) InjectLogger(l logr.Logger) error {
	p.logger = l.WithName("ruleset").WithName("eventHandler")
	return nil
}

func (p *EventHandler) Create(e event.CreateEvent, q workqueue.RateLimitingInterface) {
	obj := e.Object
	ruleSets, err := involvedRuleSets(p.client, obj)
	if err != nil {
		p.logger.Error(err, "failed to get involved rulesets for objects", "obj", commonutils.ObjectKeyString(obj))
		return
	}
	for _, rs := range ruleSets {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      rs.Name,
			Namespace: rs.Namespace,
		}})
	}
}

func (p *EventHandler) Update(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	obj := e.ObjectNew
	ruleSets, err := involvedRuleSets(p.client, obj)
	if err != nil {
		p.logger.Error(err, "failed to get involved rulesets for objects", "obj", commonutils.ObjectKeyString(obj))
		return
	}
	for _, rs := range ruleSets {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      rs.Name,
			Namespace: rs.Namespace,
		}})
	}
}

func (p *EventHandler) Delete(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
	obj := e.Object
	ruleSets, err := involvedRuleSets(p.client, obj)
	if err != nil {
		p.logger.Error(err, "failed to get involved rulesets for objects", "obj", commonutils.ObjectKeyString(obj))
		return
	}
	for _, rs := range ruleSets {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      rs.Name,
			Namespace: rs.Namespace,
		}})
	}
}

func (p *EventHandler) Generic(e event.GenericEvent, q workqueue.RateLimitingInterface) {
}

func involvedRuleSets(c client.Client, obj client.Object) ([]*appsv1alpha1.RuleSet, error) {
	ruleSetList := &appsv1alpha1.RuleSetList{}
	var ruleSets []*appsv1alpha1.RuleSet
	if err := c.List(context.TODO(), ruleSetList, client.InNamespace(obj.GetNamespace())); err != nil {
		return ruleSets, err
	}
	for i, rs := range ruleSetList.Items {
		selector, err := metav1.LabelSelectorAsSelector(rs.Spec.Selector)
		if err != nil {
			return ruleSets, err
		}
		if selector.Matches(labels.Set(obj.GetLabels())) {
			ruleSets = append(ruleSets, &ruleSetList.Items[i])
			continue
		}
		for _, item := range rs.Status.Targets {
			if item == obj.GetName() {
				ruleSets = append(ruleSets, &ruleSetList.Items[i])
			}
		}
	}
	return ruleSets, nil
}

type RulesetEventHandler struct {
}

func (p *RulesetEventHandler) Create(e event.CreateEvent, q workqueue.RateLimitingInterface) {
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      e.Object.GetName(),
		Namespace: e.Object.GetNamespace(),
	}})
}

func (p *RulesetEventHandler) Update(e event.UpdateEvent, q workqueue.RateLimitingInterface) {
	oldRuleset := e.ObjectOld.(*appsv1alpha1.RuleSet)
	newRuleset := e.ObjectNew.(*appsv1alpha1.RuleSet)
	if equality.Semantic.DeepEqual(oldRuleset.Spec, newRuleset.Spec) {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      newRuleset.Name,
		Namespace: newRuleset.Namespace,
	}})
}

func (p *RulesetEventHandler) Delete(e event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if e.Object == nil {
		return
	}
	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      e.Object.GetName(),
		Namespace: e.Object.GetNamespace(),
	}})
}

func (p *RulesetEventHandler) Generic(e event.GenericEvent, q workqueue.RateLimitingInterface) {
}
