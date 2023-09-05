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
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DemoReconcile struct {
	client.Client
}

var _ ReconcileAdapter = &DemoReconcile{}

func NewDemoReconcileAdapter(c client.Client) *DemoReconcile {
	return &DemoReconcile{
		Client: c,
	}
}

func (r *DemoReconcile) GetSelectedEmployeeNames(ctx context.Context, employer client.Object) ([]string, error) {
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

func (r *DemoReconcile) GetControllerName() string {
	return "demo-controller"
}

func (r *DemoReconcile) NotFollowPodOpsLifeCycle() bool {
	return false
}

func (r *DemoReconcile) GetExpectEmployer(ctx context.Context, employer client.Object) ([]IEmployer, error) {
	if !employer.GetDeletionTimestamp().IsZero() {
		return nil, nil
	}
	var expect []IEmployer
	expect = append(expect, DemoServiceStatus{
		EmployerId: "demo-expect-employer-id",
		EmployerStatuses: DemoServiceDetails{
			RemoteVIP:    "demo-remote-VIP",
			RemoteVIPQPS: 100,
		},
	})
	return expect, nil
}

func (r *DemoReconcile) GetCurrentEmployer(ctx context.Context, employer client.Object) ([]IEmployer, error) {
	var current []IEmployer
	if employer.GetAnnotations()["demo-current-employer"] == "" {
		return current, nil
	}
	var currentDemoServiceStatus []DemoServiceStatus
	err := json.Unmarshal([]byte(employer.GetAnnotations()["demo-current-employer"]), &currentDemoServiceStatus)
	if err != nil {
		return current, err
	}
	for _, employerStatus := range currentDemoServiceStatus {
		current = append(current, employerStatus)
	}
	return current, nil
}

func (r *DemoReconcile) CreateEmployer(employer client.Object, toCreate []IEmployer) ([]IEmployer, []IEmployer, error) {
	var currentDemoServiceStatus []DemoServiceStatus
	if employer.GetAnnotations()["demo-current-employer"] != "" {
		err := json.Unmarshal([]byte(employer.GetAnnotations()["demo-current-employer"]), &currentDemoServiceStatus)
		if err != nil {
			return nil, toCreate, err
		}
	}
	for _, create := range toCreate {
		createDemoServiceStatus, ok := create.(DemoServiceStatus)
		if !ok {
			return nil, toCreate, fmt.Errorf("toCreate employer is not DemoServiceStatus")
		}
		currentDemoServiceStatus = append(currentDemoServiceStatus, createDemoServiceStatus)
	}

	patch := client.MergeFrom(employer.DeepCopyObject().(client.Object))
	annos := employer.GetAnnotations()
	if annos == nil {
		annos = make(map[string]string)
	}
	annos["demo-current-employer"] = ""
	if currentDemoServiceStatus != nil {
		b, err := json.Marshal(currentDemoServiceStatus)
		if err != nil {
			return nil, toCreate, err
		}
		annos["demo-current-employer"] = string(b)
	}
	employer.SetAnnotations(annos)
	err := r.Patch(context.Background(), employer, patch)
	if err != nil {
		return nil, toCreate, err
	}

	return toCreate, nil, nil
}

func (r *DemoReconcile) UpdateEmployer(employer client.Object, toUpdate []IEmployer) ([]IEmployer, []IEmployer, error) {
	var currentDemoServiceStatus []DemoServiceStatus
	if employer.GetAnnotations()["demo-current-employer"] != "" {
		err := json.Unmarshal([]byte(employer.GetAnnotations()["demo-current-employer"]), &currentDemoServiceStatus)
		if err != nil {
			return nil, toUpdate, err
		}
	}

	toUpdateEmployerMap := make(map[string]IEmployer)
	updated := make(map[string]bool)
	for _, update := range toUpdate {
		toUpdateEmployerMap[update.GetEmployerId()] = update
	}

	for idx, cur := range currentDemoServiceStatus {
		if toUpdateEmployerStatus, ok := toUpdateEmployerMap[cur.EmployerId]; ok {
			currentDemoServiceStatus[idx] = toUpdateEmployerStatus.(DemoServiceStatus)
			updated[cur.EmployerId] = true
		}
	}

	patch := client.MergeFrom(employer.DeepCopyObject().(client.Object))
	annos := employer.GetAnnotations()
	annos["demo-current-employer"] = ""
	if currentDemoServiceStatus != nil {
		b, err := json.Marshal(currentDemoServiceStatus)
		if err != nil {
			return nil, toUpdate, err
		}
		annos["demo-current-employer"] = string(b)
	}
	employer.SetAnnotations(annos)
	err := r.Patch(context.Background(), employer, patch)
	if err != nil {
		return nil, toUpdate, err
	}

	var succ []IEmployer
	var fail []IEmployer
	for employerId, updatedSucc := range updated {
		if updatedSucc {
			succ = append(succ, toUpdateEmployerMap[employerId])
		} else {
			fail = append(fail, toUpdateEmployerMap[employerId])
		}
	}

	return succ, fail, nil
}

func (r *DemoReconcile) DeleteEmployer(employer client.Object, toDelete []IEmployer) ([]IEmployer, []IEmployer, error) {
	var currentDemoServiceStatus []DemoServiceStatus
	if employer.GetAnnotations()["demo-current-employer"] == "" {
		return toDelete, nil, nil
	}

	err := json.Unmarshal([]byte(employer.GetAnnotations()["demo-current-employer"]), &currentDemoServiceStatus)
	if err != nil {
		return nil, toDelete, err
	}

	toDeleteEmployerMap := make(map[string]IEmployer)
	deleted := make(map[string]bool)
	for _, del := range toDelete {
		toDeleteEmployerMap[del.GetEmployerId()] = del
	}

	var afterDeletedDemoServiceStatus []DemoServiceStatus
	for _, cur := range currentDemoServiceStatus {
		if _, ok := toDeleteEmployerMap[cur.EmployerId]; ok {
			deleted[cur.EmployerId] = true
			continue
		}
		afterDeletedDemoServiceStatus = append(afterDeletedDemoServiceStatus, cur)
	}

	patch := client.MergeFrom(employer.DeepCopyObject().(client.Object))
	annos := employer.GetAnnotations()
	if annos == nil {
		return toDelete, nil, nil
	}
	annos["demo-current-employer"] = ""
	if afterDeletedDemoServiceStatus != nil {
		b, err := json.Marshal(afterDeletedDemoServiceStatus)
		if err != nil {
			return nil, toDelete, err
		}
		annos["demo-current-employer"] = string(b)
	}

	employer.SetAnnotations(annos)
	err = r.Patch(context.Background(), employer, patch)
	if err != nil {
		return nil, toDelete, err
	}

	var succ []IEmployer
	var fail []IEmployer
	for employerId, delSucc := range deleted {
		if delSucc {
			succ = append(succ, toDeleteEmployerMap[employerId])
		} else {
			fail = append(fail, toDeleteEmployerMap[employerId])
		}
	}

	return succ, fail, nil
}

// GetExpectEmployeeStatus return expect employee status
func (r *DemoReconcile) GetExpectEmployee(ctx context.Context, employer client.Object) ([]IEmployee, error) {
	if !employer.GetDeletionTimestamp().IsZero() {
		return []IEmployee{}, nil
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

	expected := make([]IEmployee, len(podList.Items))
	expectIdx := 0
	for _, pod := range podList.Items {
		if !pod.DeletionTimestamp.IsZero() {
			continue
		}
		status := DemoPodStatus{
			EmployeeId:   pod.Name,
			EmployeeName: pod.Name,
		}
		employeeStatuses, err := GetCommonPodEmployeeStatus(&pod)
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

func (r *DemoReconcile) GetCurrentEmployee(ctx context.Context, employer client.Object) ([]IEmployee, error) {
	svc, ok := employer.(*corev1.Service)
	if !ok {
		return nil, fmt.Errorf("expect employer kind is Service")
	}

	if svc.GetAnnotations()["demo-added-pods"] == "" {
		return nil, nil
	}

	addedPodNames := strings.Split(svc.GetAnnotations()["demo-added-pods"], ",")
	current := make([]IEmployee, len(addedPodNames))
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
		employeeStatus, err := GetCommonPodEmployeeStatus(pod)
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

func (r *DemoReconcile) CreateEmployees(employer client.Object, toCreate []IEmployee) ([]IEmployee, []IEmployee, error) {
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

func (r *DemoReconcile) UpdateEmployees(employer client.Object, toUpdate []IEmployee) ([]IEmployee, []IEmployee, error) {
	if employer == nil {
		return nil, nil, fmt.Errorf("employer is nil")
	}
	if len(toUpdate) == 0 {
		return toUpdate, nil, nil
	}
	succUpdate := make([]IEmployee, len(toUpdate))
	failUpdate := make([]IEmployee, len(toUpdate))
	succUpdateIdx, failUpdateIdx := 0, 0
	for _, employee := range toUpdate {
		pod := &corev1.Pod{}
		err := r.Get(context.Background(), types.NamespacedName{
			Namespace: employer.GetNamespace(),
			Name:      employee.GetEmployeeName(),
		}, pod)
		podEmployeeStatus := employee.GetEmployeeStatuses().(PodEmployeeStatuses)
		if err != nil {
			return succUpdate, failUpdate, err
		}
		extraStatus := podEmployeeStatus.ExtraStatus.(PodExtraStatus)
		if extraStatus.TrafficOn {
			pod.GetLabels()["demo-traffic-on"] = "true"
		} else {
			pod.GetLabels()["demo-traffic-on"] = "false"
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

func (r *DemoReconcile) DeleteEmployees(employer client.Object, toDelete []IEmployee) ([]IEmployee, []IEmployee, error) {
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

var _ IEmployer = DemoServiceStatus{}
var _ IEmployee = DemoPodStatus{}

type DemoServiceStatus struct {
	EmployerId       string
	EmployerStatuses DemoServiceDetails
}

type DemoServiceDetails struct {
	RemoteVIP    string
	RemoteVIPQPS int
}

func (d DemoServiceStatus) GetEmployerId() string {
	return d.EmployerId
}

func (d DemoServiceStatus) GetEmployerStatuses() interface{} {
	return d.EmployerStatuses
}

func (d DemoServiceStatus) EmployerEqual(employer IEmployer) (bool, error) {
	if d.EmployerId != employer.GetEmployerId() {
		return false, nil
	}
	employerStatus, ok := employer.GetEmployerStatuses().(DemoServiceDetails)
	if !ok {
		return false, fmt.Errorf("employer to diff is not Demo Service Details")
	}
	return reflect.DeepEqual(d.EmployerStatuses, employerStatus), nil
}

type DemoPodStatus struct {
	EmployeeId       string
	EmployeeName     string
	EmployeeStatuses PodEmployeeStatuses
}

func (d DemoPodStatus) GetEmployeeId() string {
	return d.EmployeeId
}

func (d DemoPodStatus) GetEmployeeName() string {
	return d.EmployeeName
}

func (d DemoPodStatus) GetEmployeeStatuses() interface{} {
	return d.EmployeeStatuses
}

func (d DemoPodStatus) EmployeeEqual(employeeStatus IEmployee) (bool, error) {
	if d.EmployeeName != employeeStatus.GetEmployeeName() {
		return false, nil
	}

	podEmployeeStatuses, ok := employeeStatus.GetEmployeeStatuses().(PodEmployeeStatuses)
	if !ok {
		return false, fmt.Errorf("employee to diff is not Pod Employee status")
	}

	if d.EmployeeStatuses.Ip != podEmployeeStatuses.Ip || d.EmployeeStatuses.LifecycleReady != podEmployeeStatuses.LifecycleReady ||
		d.EmployeeStatuses.Ipv6 != podEmployeeStatuses.Ipv6 {
		return false, nil
	}

	return reflect.DeepEqual(d.EmployeeStatuses.ExtraStatus, podEmployeeStatuses.ExtraStatus), nil
}

type PodExtraStatus struct {
	TrafficOn     bool
	TrafficWeight int
}
