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

package demoresourceconsist

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"kusionstack.io/kafed/pkg/controllers/resourceconsist"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

type ReconcileAdapter struct {
	client.Client
}

var _ resourceconsist.ReconcileAdapter = &ReconcileAdapter{}

func NewReconcileAdapter(c client.Client) *ReconcileAdapter {
	return &ReconcileAdapter{
		Client: c,
	}
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

func (r *ReconcileAdapter) GetControllerName() string {
	return "demo-controller"
}

func (r *ReconcileAdapter) NewEmployer() client.Object {
	return &corev1.Service{}
}

func (r *ReconcileAdapter) NewEmployee() client.Object {
	return &corev1.Pod{}
}

func (r *ReconcileAdapter) EmployerEventHandler() handler.EventHandler {
	return &EnqueueServiceWithRateLimit{}
}

func (r *ReconcileAdapter) EmployeeEventHandler() handler.EventHandler {
	return &EnqueueServiceByPod{
		c: r.Client,
	}
}

func (r *ReconcileAdapter) EmployerPredicates() predicate.Funcs {
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

func (r *ReconcileAdapter) EmployeePredicates() predicate.Funcs {
	return predicate.Funcs{}
}

func (r *ReconcileAdapter) NotFollowPodOpsLifeCycle() bool {
	return false
}

func (r *ReconcileAdapter) GetExpectEmployer(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployer, error) {
	return nil, nil
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

// GetExpectEmployeeStatus return expect employee status
func (r *ReconcileAdapter) GetExpectEmployee(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployee, error) {
	if !employer.GetDeletionTimestamp().IsZero() {
		return []resourceconsist.IEmployee{}, nil
	}

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
	expectIdx := 0
	for _, pod := range podList.Items {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		status := DemoPodStatus{
			EmployeeId:   pod.Name,
			EmployeeName: pod.Name,
		}
		employeeStatuses, err := resourceconsist.GetCommonPodEmployeeStatus(&pod)
		if err != nil {
			return nil, err
		}
		extraStatus := PodExtraStatus{}
		if employeeStatuses.LifecycleReady {
			extraStatus.TrafficOn = true
			extraStatus.TrafficWeight = 100
		} else {
			extraStatus.TrafficOn = false
			extraStatus.TrafficWeight = 0
		}
		employeeStatuses.ExtraStatus = extraStatus
		status.EmployeeStatuses = employeeStatuses
		expected[expectIdx] = status
		expectIdx++
	}

	return expected[:expectIdx], nil
}

func (r *ReconcileAdapter) GetCurrentEmployee(ctx context.Context, employer client.Object) ([]resourceconsist.IEmployee, error) {
	svc, ok := employer.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expect employer kind is Service")
	}

	if svc.GetAnnotations()["demo-added-pods"] == "" {
		return nil, nil
	}

	addedPodNames := strings.Split(svc.GetAnnotations()["demo-added-pods"], ",")
	current := make([]resourceconsist.IEmployee, len(addedPodNames))
	currentIdx := 0

	for _, podName := range addedPodNames {
		pod := &corev1.Pod{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: employer.GetNamespace(),
			Name:      podName,
		}, pod)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, err
		}
		status := DemoPodStatus{
			EmployeeId:   podName,
			EmployeeName: podName,
		}
		employeeStatus, err := resourceconsist.GetCommonPodEmployeeStatus(pod)
		if err != nil {
			return nil, err
		}
		extraStatus := PodExtraStatus{}
		if pod.GetLabels()["demo-traffic-on"] == "true" {
			extraStatus.TrafficOn = true
		}
		if pod.GetLabels()["demo-traffic-weight"] != "" {
			extraStatus.TrafficWeight, _ = strconv.Atoi(pod.GetLabels()["demo-traffic-weight"])
		}
		employeeStatus.ExtraStatus = extraStatus
		status.EmployeeStatuses = employeeStatus
		current[currentIdx] = status
		currentIdx++
	}
	return current[:currentIdx], nil
}

func (r *ReconcileAdapter) CreateEmployees(employer client.Object, toCreate []resourceconsist.IEmployee) ([]resourceconsist.IEmployee, []resourceconsist.IEmployee, error) {
	if employer == nil {
		return nil, nil, fmt.Errorf("employer is nil")
	}
	if len(toCreate) == 0 {
		return toCreate, nil, nil
	}
	toAddNames := make([]string, len(toCreate))
	toAddIdx := 0
	for _, employee := range toCreate {
		toAddNames[toAddIdx] = employee.GetEmployeeName()
		toAddIdx++
	}
	toAddNames = toAddNames[:toAddIdx]
	anno := employer.GetAnnotations()
	if anno["demo-added-pods"] == "" {
		anno["demo-added-pods"] = strings.Join(toAddNames, ",")
	} else {
		anno["demo-added-pods"] = anno["demo-added-pods"] + "," + strings.Join(toAddNames, ",")
	}
	employer.SetAnnotations(anno)
	err := r.Client.Update(context.Background(), employer)
	if err != nil {
		return nil, nil, err
	}
	return toCreate, nil, nil
}

func (r *ReconcileAdapter) UpdateEmployees(employer client.Object, toUpdate []resourceconsist.IEmployee) ([]resourceconsist.IEmployee, []resourceconsist.IEmployee, error) {
	if employer == nil {
		return nil, nil, fmt.Errorf("employer is nil")
	}
	if len(toUpdate) == 0 {
		return toUpdate, nil, nil
	}
	succUpdate := make([]resourceconsist.IEmployee, len(toUpdate))
	failUpdate := make([]resourceconsist.IEmployee, len(toUpdate))
	succUpdateIdx, failUpdateIdx := 0, 0
	for _, employee := range toUpdate {
		pod := &corev1.Pod{}
		err := r.Get(context.Background(), types.NamespacedName{
			Namespace: employer.GetNamespace(),
			Name:      employee.GetEmployeeName(),
		}, pod)
		podEmployeeStatus := employee.GetEmployeeStatuses().(resourceconsist.PodEmployeeStatuses)
		if err != nil {
			return succUpdate, failUpdate, err
		}
		extraStatus := podEmployeeStatus.ExtraStatus.(PodExtraStatus)
		if extraStatus.TrafficOn {
			pod.GetLabels()["demo-traffic-on"] = "true"
		}
		pod.GetLabels()["demo-traffic-weight"] = strconv.Itoa(extraStatus.TrafficWeight)
		err = r.Client.Update(context.Background(), pod)
		if err != nil {
			failUpdate[failUpdateIdx] = employee
			failUpdateIdx++
			continue
		}
		succUpdate[succUpdateIdx] = employee
		succUpdateIdx++
	}
	return succUpdate[:succUpdateIdx], failUpdate[:failUpdateIdx], nil
}

func (r *ReconcileAdapter) DeleteEmployees(employer client.Object, toDelete []resourceconsist.IEmployee) ([]resourceconsist.IEmployee, []resourceconsist.IEmployee, error) {
	if employer == nil {
		return nil, nil, fmt.Errorf("employer is nil")
	}
	if len(toDelete) == 0 {
		return toDelete, nil, nil
	}

	toDeleteMap := make(map[string]bool)
	for _, employee := range toDelete {
		toDeleteMap[employee.GetEmployeeName()] = true
	}

	addedPodNames := strings.Split(employer.GetAnnotations()["demo-added-pods"], ",")

	afterDeleteIdx := 0
	for _, added := range addedPodNames {
		if !toDeleteMap[added] {
			addedPodNames[afterDeleteIdx] = added
			afterDeleteIdx++
		}
	}
	addedPodNames = addedPodNames[:afterDeleteIdx]
	anno := employer.GetAnnotations()
	anno["demo-added-pods"] = strings.Join(addedPodNames, ",")
	employer.SetAnnotations(anno)
	err := r.Client.Update(context.Background(), employer)
	if err != nil {
		return nil, nil, err
	}
	return toDelete, nil, nil
}
