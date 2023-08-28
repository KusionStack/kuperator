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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/pkg/controllers/resourceconsist"
)

var _ resourceconsist.ReconcileAdapter = &ReconcileAdapter{}

type ReconcileAdapter struct {
	client.Client
}

func NewReconcileAdapter(c client.Client) *ReconcileAdapter {
	return &ReconcileAdapter{
		Client: c,
	}
}

func (r *ReconcileAdapter) GetControllerName() string {
	return "alibaba-cloud-slb-controller"
}

func (r *ReconcileAdapter) NotFollowPodOpsLifeCycle() bool {
	return false
}

func (r *ReconcileAdapter) GetExpectEmployer(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployer, error) {
	return nil, nil
}

func (r *ReconcileAdapter) GetSelectedEmployeeNames(ctx context.Context, employer client.Object) ([]string, error) {
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

	selected := make([]string, len(podList.Items))
	for idx, pod := range podList.Items {
		selected[idx] = pod.Name
	}

	return selected, nil
}

func (r *ReconcileAdapter) GetCurrentEmployer(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployer, error) {
	return nil, nil
}

func (r *ReconcileAdapter) CreateEmployer(employer client.Object, toCreate []resourceconsist.IEmployer) ([]resourceconsist.IEmployer, []resourceconsist.IEmployer, error) {
	return nil, nil, nil
}

func (r *ReconcileAdapter) UpdateEmployer(employer client.Object, toUpdate []resourceconsist.IEmployer) ([]resourceconsist.IEmployer, []resourceconsist.IEmployer, error) {
	return nil, nil, nil
}

func (r *ReconcileAdapter) DeleteEmployer(employer client.Object, toDelete []resourceconsist.IEmployer) ([]resourceconsist.IEmployer, []resourceconsist.IEmployer, error) {
	return nil, nil, nil
}

func (r *ReconcileAdapter) RecordEmployer(succCreate, succUpdate, succDelete []resourceconsist.IEmployer) error {
	return nil
}

func (r *ReconcileAdapter) GetExpectEmployee(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployee, error) {
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

	expected := make([]resourceconsist.IEmployee, len(podList.Items))
	for idx, pod := range podList.Items {
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
		employeeStatuses.ExtraStatus = extraStatus
		status.EmployeeStatuses = employeeStatuses
		expected[idx] = status
	}

	return expected, nil
}

func (r *ReconcileAdapter) GetCurrentEmployee(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployee, error) {
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

	lbID := svc.GetLabels()[alibabaCloudSlbLbIdLabelKey]
	bsExistUnderSlb := make(map[string]bool)
	if lbID != "" {
		backendServers, err := alibabaCloudSlbClient.GetBackendServers(lbID)
		if err != nil {
			return nil, fmt.Errorf("get backend servers of slb failed, err: %s", err.Error())
		}
		for _, bs := range backendServers {
			bsExistUnderSlb[bs] = true
		}
	}

	current := make([]resourceconsist.IEmployee, len(podList.Items))
	for idx, pod := range podList.Items {
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
		current[idx] = status
	}

	return current, nil
}

// CreateEmployees returns (nil, toCreate, nil) since CCM of ACK will sync bs of slb
func (r *ReconcileAdapter) CreateEmployees(employer client.Object, toCreate []resourceconsist.IEmployee) ([]resourceconsist.IEmployee, []resourceconsist.IEmployee, error) {
	return nil, toCreate, nil
}

// UpdateEmployees returns (nil, toUpdate, nil) since CCM of ACK will sync bs of slb
func (r *ReconcileAdapter) UpdateEmployees(employer client.Object, toUpdate []resourceconsist.IEmployee) ([]resourceconsist.IEmployee, []resourceconsist.IEmployee, error) {
	return nil, toUpdate, nil
}

// DeleteEmployees returns (nil, toDelete, nil) since CCM of ACK will sync bs of slb
func (r *ReconcileAdapter) DeleteEmployees(employer client.Object, toDelete []resourceconsist.IEmployee) ([]resourceconsist.IEmployee, []resourceconsist.IEmployee, error) {
	return nil, toDelete, nil
}
