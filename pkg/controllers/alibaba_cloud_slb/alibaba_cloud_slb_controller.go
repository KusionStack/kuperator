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

package alibaba_cloud_slb

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/labels"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/workqueue"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"

	"kusionstack.io/kafed/pkg/controllers/resourceconsist"
)

var _ resourceconsist.ReconcileAdapter = &ReconcileAdapter{}

type ReconcileAdapter struct {
	client.Client
}

func (r *ReconcileAdapter) GetRateLimiter() ratelimiter.RateLimiter {
	return workqueue.DefaultControllerRateLimiter()
}

func (r *ReconcileAdapter) GetMaxConcurrentReconciles() int {
	return 5
}

func (r *ReconcileAdapter) EmployerResource() client.Object {
	return &corev1.Service{}
}

func (r *ReconcileAdapter) EmployeeResource() client.Object {
	return &corev1.Pod{}
}

func (r *ReconcileAdapter) EmployerResourceHandler() handler.EventHandler {
	return &EnqueueServiceWithRateLimit{}
}

func (r *ReconcileAdapter) EmployeeResourceHandler() handler.EventHandler {
	return &EnqueueServiceByPod{
		c: r.Client,
	}
}

func (r *ReconcileAdapter) EmployerResourcePredicates() predicate.Funcs {
	return predicate.Funcs{
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
}

func (r *ReconcileAdapter) EmployeeResourcePredicates() predicate.Funcs {
	return predicate.Funcs{}
}

func (r *ReconcileAdapter) NotFollowPodOpsLifeCycle() bool {
	return false
}

func (r *ReconcileAdapter) GetExpectEmployerStatus(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployerStatus, error) {
	return nil, nil
}

func (r *ReconcileAdapter) GetCurrentEmployerStatus(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployerStatus, error) {
	return nil, nil
}

func (r *ReconcileAdapter) CreateEmployer(employer client.Object, toCreate []resourceconsist.IEmployerStatus) ([]resourceconsist.IEmployerStatus, []resourceconsist.IEmployerStatus, error) {
	return nil, nil, nil
}

func (r *ReconcileAdapter) UpdateEmployer(employer client.Object, toUpdate []resourceconsist.IEmployerStatus) ([]resourceconsist.IEmployerStatus, []resourceconsist.IEmployerStatus, error) {
	return nil, nil, nil
}

func (r *ReconcileAdapter) DeleteEmployer(employer client.Object, toDelete []resourceconsist.IEmployerStatus) ([]resourceconsist.IEmployerStatus, []resourceconsist.IEmployerStatus, error) {
	return nil, nil, nil
}

func (r *ReconcileAdapter) RecordEmployer(succCreate, succUpdate, succDelete []resourceconsist.IEmployerStatus) error {
	return nil
}

func (r *ReconcileAdapter) GetExpectEmployeeStatus(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployeeStatus, error) {
	svc, ok := employer.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expect employer kind is Service")
	}
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	var podList corev1.PodList
	err := r.List(ctx, &podList, &client.ListOptions{Namespace: svc.Namespace, LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	expected := make([]resourceconsist.IEmployeeStatus, len(podList.Items))
	expectIdx := 0
	for _, pod := range podList.Items {
		status := AlibabaSlbPodStatus{
			EmployeeID:   pod.Status.PodIP,
			EmployeeName: pod.Name,
		}
		employeeStatuses, err := resourceconsist.GetCommonPodEmployeeStatus(&pod)
		if err != nil {
			return nil, err
		}
		extraStatus := PodExtraStatus{}
		if employeeStatuses.LifecycleReady {
			extraStatus.TrafficOn = true
		} else {
			extraStatus.TrafficOn = false
		}
		if !pod.DeletionTimestamp.IsZero() {
			extraStatus.TrafficOn = false
		}
		employeeStatuses.ExtraStatus = extraStatus
		status.EmployeeStatuses = employeeStatuses
		expected[expectIdx] = status
		expectIdx++
	}

	return expected[:expectIdx], nil
}

func (r *ReconcileAdapter) GetCurrentEmployeeStatus(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployeeStatus, error) {
	svc, ok := employer.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expect employer kind is Service")
	}
	selector := labels.Set(svc.Spec.Selector).AsSelectorPreValidated()
	var podList corev1.PodList
	err := r.List(ctx, &podList, &client.ListOptions{Namespace: svc.Namespace, LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	lbID := svc.GetLabels()["service.k8s.alibaba/loadbalancer-id"]
	bsExistUnderSlb := make(map[string]bool)
	if lbID != "" {
		alibabaCloudSlbClient, err := NewAlibabaCloudSlbClient()
		if err != nil {
			return nil, err
		}
		backendServers, err := alibabaCloudSlbClient.GetBackendServers(lbID)
		for _, bs := range backendServers {
			bsExistUnderSlb[bs] = true
		}
	}

	current := make([]resourceconsist.IEmployeeStatus, len(podList.Items))
	currentIdx := 0
	for _, pod := range podList.Items {
		status := AlibabaSlbPodStatus{
			EmployeeID:   pod.Status.PodIP,
			EmployeeName: pod.Name,
		}
		employeeStatuses, err := resourceconsist.GetCommonPodEmployeeStatus(&pod)
		if err != nil {
			return nil, err
		}
		extraStatus := PodExtraStatus{}
		if !bsExistUnderSlb[status.EmployeeID] {
			extraStatus.TrafficOn = false
		} else {
			extraStatus.TrafficOn = true
		}
		employeeStatuses.ExtraStatus = extraStatus
		status.EmployeeStatuses = employeeStatuses
		current[currentIdx] = status
		currentIdx++
	}

	return current[:currentIdx], nil
}

// CreateEmployees returns (nil, toCreate, nil) since CCM of ACK will sync bs of slb
func (r *ReconcileAdapter) CreateEmployees(employer client.Object, toCreate []resourceconsist.IEmployeeStatus) ([]resourceconsist.IEmployeeStatus, []resourceconsist.IEmployeeStatus, error) {
	return nil, toCreate, nil
}

// UpdateEmployees returns (nil, toUpdate, nil) since CCM of ACK will sync bs of slb
func (r *ReconcileAdapter) UpdateEmployees(employer client.Object, toUpdate []resourceconsist.IEmployeeStatus) ([]resourceconsist.IEmployeeStatus, []resourceconsist.IEmployeeStatus, error) {
	return nil, toUpdate, nil
}

// DeleteEmployees returns (nil, toDelete, nil) since CCM of ACK will sync bs of slb
func (r *ReconcileAdapter) DeleteEmployees(employer client.Object, toDelete []resourceconsist.IEmployeeStatus) ([]resourceconsist.IEmployeeStatus, []resourceconsist.IEmployeeStatus, error) {
	return nil, toDelete, nil
}
