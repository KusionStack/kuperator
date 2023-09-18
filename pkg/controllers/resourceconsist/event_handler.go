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

package resourceconsist

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"kusionstack.io/operating/apis/apps/v1alpha1"
)

var _ handler.EventHandler = &EnqueueServiceWithRateLimit{}

var _ handler.EventHandler = &EnqueueServiceByPod{}

type EnqueueServiceWithRateLimit struct{}

type EnqueueServiceByPod struct {
	c client.Client
}

func (e *EnqueueServiceWithRateLimit) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}

	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      evt.Object.GetName(),
		Namespace: evt.Object.GetNamespace(),
	}})
}

func (e *EnqueueServiceWithRateLimit) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	if evt.ObjectOld != nil {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      evt.ObjectOld.GetName(),
			Namespace: evt.ObjectOld.GetNamespace(),
		}})
	}

	if evt.ObjectNew != nil {
		q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
			Name:      evt.ObjectNew.GetName(),
			Namespace: evt.ObjectNew.GetNamespace(),
		}})
	}
}

func (e *EnqueueServiceWithRateLimit) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}

	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      evt.Object.GetName(),
		Namespace: evt.Object.GetNamespace(),
	}})
}

func (e *EnqueueServiceWithRateLimit) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}

	q.Add(reconcile.Request{NamespacedName: types.NamespacedName{
		Name:      evt.Object.GetName(),
		Namespace: evt.Object.GetNamespace(),
	}})
}

func (e *EnqueueServiceByPod) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	serviceNames, err := GetEmployerByEmployee(context.Background(), e.c, evt.Object)
	if err != nil {
		return
	}
	for _, obj := range serviceNames {
		svc := &corev1.Service{}
		err = e.c.Get(context.Background(), types.NamespacedName{Name: obj, Namespace: evt.Object.GetNamespace()}, svc)
		if err != nil {
			continue
		}
		if doPredicate(svc) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: evt.Object.GetNamespace(), Name: obj}})
		}
	}
}

func (e *EnqueueServiceByPod) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	serviceNamesOld, err := GetEmployerByEmployee(context.Background(), e.c, evt.ObjectOld)
	if err != nil {
		return
	}
	for _, obj := range serviceNamesOld {
		svc := &corev1.Service{}
		err = e.c.Get(context.Background(), types.NamespacedName{Name: obj, Namespace: evt.ObjectOld.GetNamespace()}, svc)
		if err != nil {
			continue
		}
		if doPredicate(svc) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: evt.ObjectOld.GetNamespace(), Name: obj}})
		}
	}

	serviceNamesNew, err := GetEmployerByEmployee(context.Background(), e.c, evt.ObjectNew)
	if err != nil {
		return
	}
	for _, obj := range serviceNamesNew {
		svc := &corev1.Service{}
		err = e.c.Get(context.Background(), types.NamespacedName{Name: obj, Namespace: evt.ObjectNew.GetNamespace()}, svc)
		if err != nil {
			continue
		}
		if doPredicate(svc) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: evt.ObjectNew.GetNamespace(), Name: obj}})
		}
	}
}

func (e *EnqueueServiceByPod) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	serviceNames, err := GetEmployerByEmployee(context.Background(), e.c, evt.Object)
	if err != nil {
		return
	}
	for _, obj := range serviceNames {
		svc := &corev1.Service{}
		err = e.c.Get(context.Background(), types.NamespacedName{Name: obj, Namespace: evt.Object.GetNamespace()}, svc)
		if err != nil {
			continue
		}
		if doPredicate(svc) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: evt.Object.GetNamespace(), Name: obj}})
		}
	}
}

func (e *EnqueueServiceByPod) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	serviceNames, err := GetEmployerByEmployee(context.Background(), e.c, evt.Object)
	if err != nil {
		return
	}
	for _, obj := range serviceNames {
		svc := &corev1.Service{}
		err = e.c.Get(context.Background(), types.NamespacedName{Name: obj, Namespace: evt.Object.GetNamespace()}, svc)
		if err != nil {
			continue
		}
		if doPredicate(svc) {
			q.Add(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: evt.Object.GetNamespace(), Name: obj}})
		}
	}
}

func GetEmployerByEmployee(ctx context.Context, client client.Client, employee client.Object) ([]string, error) {
	if employee.GetLabels() == nil {
		return nil, nil
	}

	services := &corev1.ServiceList{}
	if err := client.List(ctx, services); err != nil {
		return nil, err
	}

	employeeLabels := labels.Set(employee.GetLabels())
	var names []string
	for _, svc := range services.Items {
		selector := labels.Set(svc.Spec.Selector).AsSelector()
		if selector.Matches(employeeLabels) {
			names = append(names, svc.GetName())
		}
	}

	return names, nil
}

func doPredicate(obj client.Object) bool {
	if obj == nil {
		return false
	}
	if obj.GetLabels() != nil {
		value, exist := obj.GetLabels()[v1alpha1.ControlledByKusionStackLabelKey]
		if exist && value == "true" {
			return true
		}
	}
	return false
}

var employerPredicates = predicate.Funcs{
	CreateFunc: func(event event.CreateEvent) bool {
		return doPredicate(event.Object)
	},
	DeleteFunc: func(event event.DeleteEvent) bool {
		return doPredicate(event.Object)
	},
	UpdateFunc: func(event event.UpdateEvent) bool {
		return doPredicate(event.ObjectNew)
	},
	GenericFunc: func(event event.GenericEvent) bool {
		return doPredicate(event.Object)
	},
}

var employeePredicates = predicate.Funcs{}
