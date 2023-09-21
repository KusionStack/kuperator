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
	"fmt"
	"reflect"
	"sync"

	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DemoReconcile struct {
	client.Client
	resourceProviderClient *DemoResourceProviderClient
}

var _ ReconcileAdapter = &DemoReconcile{}

func NewDemoReconcileAdapter(c client.Client, rc *DemoResourceProviderClient) *DemoReconcile {
	return &DemoReconcile{
		Client:                 c,
		resourceProviderClient: rc,
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

func (r *DemoReconcile) GetExpectedEmployer(ctx context.Context, employer client.Object) ([]IEmployer, error) {
	if !employer.GetDeletionTimestamp().IsZero() {
		return nil, nil
	}
	var expect []IEmployer
	expect = append(expect, DemoServiceStatus{
		EmployerId: employer.GetName(),
		EmployerStatuses: DemoServiceDetails{
			RemoteVIP:    "demo-remote-VIP",
			RemoteVIPQPS: 100,
		},
	})
	return expect, nil
}

func (r *DemoReconcile) GetCurrentEmployer(ctx context.Context, employer client.Object) ([]IEmployer, error) {
	var current []IEmployer

	req := &DemoResourceVipOps{}
	resp, err := r.resourceProviderClient.QueryVip(req)
	if err != nil {
		return current, err
	}
	if resp == nil {
		return current, fmt.Errorf("demo resource vip query resp is nil")
	}

	for _, employerStatus := range resp.VipStatuses {
		current = append(current, employerStatus)
	}
	return current, nil
}

func (r *DemoReconcile) CreateEmployer(ctx context.Context, employer client.Object, toCreates []IEmployer) ([]IEmployer, []IEmployer, error) {
	if toCreates == nil || len(toCreates) == 0 {
		return toCreates, nil, nil
	}

	toCreateDemoServiceStatus := make([]DemoServiceStatus, len(toCreates))
	for idx, create := range toCreates {
		createDemoServiceStatus, ok := create.(DemoServiceStatus)
		if !ok {
			return nil, toCreates, fmt.Errorf("toCreates employer is not DemoServiceStatus")
		}
		toCreateDemoServiceStatus[idx] = createDemoServiceStatus
	}

	_, err := r.resourceProviderClient.CreateVip(&DemoResourceVipOps{
		VipStatuses: toCreateDemoServiceStatus,
	})
	if err != nil {
		return nil, toCreates, err
	}
	return toCreates, nil, nil
}

func (r *DemoReconcile) UpdateEmployer(ctx context.Context, employer client.Object, toUpdates []IEmployer) ([]IEmployer, []IEmployer, error) {
	if toUpdates == nil || len(toUpdates) == 0 {
		return toUpdates, nil, nil
	}

	toUpdateDemoServiceStatus := make([]DemoServiceStatus, len(toUpdates))
	for idx, update := range toUpdates {
		updateDemoServiceStatus, ok := update.(DemoServiceStatus)
		if !ok {
			return nil, toUpdates, fmt.Errorf("toUpdates employer is not DemoServiceStatus")
		}
		toUpdateDemoServiceStatus[idx] = updateDemoServiceStatus
	}

	_, err := r.resourceProviderClient.UpdateVip(&DemoResourceVipOps{
		VipStatuses: toUpdateDemoServiceStatus,
	})
	if err != nil {
		return nil, toUpdates, err
	}
	return toUpdates, nil, nil
}

func (r *DemoReconcile) DeleteEmployer(ctx context.Context, employer client.Object, toDeletes []IEmployer) ([]IEmployer, []IEmployer, error) {
	if toDeletes == nil || len(toDeletes) == 0 {
		return toDeletes, nil, nil
	}

	toDeleteDemoServiceStatus := make([]DemoServiceStatus, len(toDeletes))
	for idx, update := range toDeletes {
		deleteDemoServiceStatus, ok := update.(DemoServiceStatus)
		if !ok {
			return nil, toDeletes, fmt.Errorf("toDeletes employer is not DemoServiceStatus")
		}
		toDeleteDemoServiceStatus[idx] = deleteDemoServiceStatus
	}

	_, err := r.resourceProviderClient.DeleteVip(&DemoResourceVipOps{
		VipStatuses: toDeleteDemoServiceStatus,
	})
	if err != nil {
		return nil, toDeletes, err
	}
	return toDeletes, nil, nil
}

// GetExpectEmployeeStatus return expect employee status
func (r *DemoReconcile) GetExpectedEmployee(ctx context.Context, employer client.Object) ([]IEmployee, error) {
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
	var current []IEmployee
	req := &DemoResourceRsOps{}
	resp, err := r.resourceProviderClient.QueryRealServer(req)
	if err != nil {
		return current, err
	}
	if resp == nil {
		return current, fmt.Errorf("demo resource rs query resp is nil")
	}

	for _, rsStatus := range resp.RsStatuses {
		current = append(current, rsStatus)
	}
	return current, nil
}

func (r *DemoReconcile) CreateEmployees(ctx context.Context, employer client.Object, toCreates []IEmployee) ([]IEmployee, []IEmployee, error) {
	if toCreates == nil || len(toCreates) == 0 {
		return toCreates, nil, nil
	}
	toCreateDemoPodStatuses := make([]DemoPodStatus, len(toCreates))

	for idx, toCreate := range toCreates {
		podStatus, ok := toCreate.(DemoPodStatus)
		if !ok {
			return nil, toCreates, fmt.Errorf("toCreate is not DemoPodStatus")
		}
		toCreateDemoPodStatuses[idx] = podStatus
	}

	_, err := r.resourceProviderClient.CreateRealServer(&DemoResourceRsOps{
		RsStatuses: toCreateDemoPodStatuses,
	})
	if err != nil {
		return nil, toCreates, err
	}

	return toCreates, nil, nil
}

func (r *DemoReconcile) UpdateEmployees(ctx context.Context, employer client.Object, toUpdates []IEmployee) ([]IEmployee, []IEmployee, error) {
	if toUpdates == nil || len(toUpdates) == 0 {
		return toUpdates, nil, nil
	}

	toUpdateDemoPodStatuses := make([]DemoPodStatus, len(toUpdates))

	for idx, toUpdate := range toUpdates {
		podStatus, ok := toUpdate.(DemoPodStatus)
		if !ok {
			return nil, toUpdates, fmt.Errorf("toUpdate is not DemoPodStatus")
		}
		toUpdateDemoPodStatuses[idx] = podStatus
	}

	_, err := r.resourceProviderClient.UpdateRealServer(&DemoResourceRsOps{
		RsStatuses: toUpdateDemoPodStatuses,
	})
	if err != nil {
		return nil, toUpdates, err
	}

	return toUpdates, nil, nil
}

func (r *DemoReconcile) DeleteEmployees(ctx context.Context, employer client.Object, toDeletes []IEmployee) ([]IEmployee, []IEmployee, error) {
	if toDeletes == nil || len(toDeletes) == 0 {
		return toDeletes, nil, nil
	}

	toDeleteDemoPodStatuses := make([]DemoPodStatus, len(toDeletes))

	for idx, toDelete := range toDeletes {
		podStatus, ok := toDelete.(DemoPodStatus)
		if !ok {
			return nil, toDeletes, fmt.Errorf("toDelete is not DemoPodStatus")
		}
		toDeleteDemoPodStatuses[idx] = podStatus
	}

	_, err := r.resourceProviderClient.DeleteRealServer(&DemoResourceRsOps{
		RsStatuses: toDeleteDemoPodStatuses,
	})
	if err != nil {
		return nil, toDeletes, err
	}

	return toDeletes, nil, nil
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

var demoResourceVipStatusInProvider sync.Map
var demoResourceRsStatusInProvider sync.Map

type DemoResourceProviderClient struct {
	mock.Mock
}

type DemoResourceVipOps struct {
	VipStatuses []DemoServiceStatus
	MockData    bool
}

type DemoResourceRsOps struct {
	RsStatuses []DemoPodStatus
	MockData   bool
}

func (d *DemoResourceProviderClient) CreateVip(req *DemoResourceVipOps) (*DemoResourceVipOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceVipOps).MockData {
		for _, vipStatus := range req.VipStatuses {
			demoResourceVipStatusInProvider.Store(vipStatus.EmployerId, vipStatus.EmployerStatuses)
		}
	}
	return args.Get(0).(*DemoResourceVipOps), args.Error(1)
}

func (d *DemoResourceProviderClient) UpdateVip(req *DemoResourceVipOps) (*DemoResourceVipOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceVipOps).MockData {
		for _, vipStatus := range req.VipStatuses {
			demoResourceVipStatusInProvider.Store(vipStatus.EmployerId, vipStatus.EmployerStatuses)
		}
	}
	return args.Get(0).(*DemoResourceVipOps), args.Error(1)
}

func (d *DemoResourceProviderClient) DeleteVip(req *DemoResourceVipOps) (*DemoResourceVipOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceVipOps).MockData {
		for _, vipStatus := range req.VipStatuses {
			demoResourceVipStatusInProvider.Delete(vipStatus.EmployerId)
		}
	}
	return args.Get(0).(*DemoResourceVipOps), args.Error(1)
}

func (d *DemoResourceProviderClient) QueryVip(req *DemoResourceVipOps) (*DemoResourceVipOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceVipOps).MockData {
		vipStatuses := make([]DemoServiceStatus, 0)
		demoResourceVipStatusInProvider.Range(func(key, value any) bool {
			vipStatuses = append(vipStatuses, DemoServiceStatus{
				EmployerId:       key.(string),
				EmployerStatuses: value.(DemoServiceDetails),
			})
			return true
		})
		return &DemoResourceVipOps{
			VipStatuses: vipStatuses,
		}, args.Error(1)
	}
	return args.Get(0).(*DemoResourceVipOps), args.Error(1)
}

func (d *DemoResourceProviderClient) CreateRealServer(req *DemoResourceRsOps) (*DemoResourceRsOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceRsOps).MockData {
		for _, employeeStatus := range req.RsStatuses {
			demoResourceRsStatusInProvider.Store(employeeStatus.GetEmployeeId(), employeeStatus)
		}
	}
	return args.Get(0).(*DemoResourceRsOps), args.Error(1)
}

func (d *DemoResourceProviderClient) UpdateRealServer(req *DemoResourceRsOps) (*DemoResourceRsOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceRsOps).MockData {
		for _, employeeStatus := range req.RsStatuses {
			demoResourceRsStatusInProvider.Store(employeeStatus.GetEmployeeId(), employeeStatus)
		}
	}
	return args.Get(0).(*DemoResourceRsOps), args.Error(1)
}

func (d *DemoResourceProviderClient) DeleteRealServer(req *DemoResourceRsOps) (*DemoResourceRsOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceRsOps).MockData {
		for _, employeeStatus := range req.RsStatuses {
			demoResourceRsStatusInProvider.Delete(employeeStatus.GetEmployeeId())
		}
	}
	return args.Get(0).(*DemoResourceRsOps), args.Error(1)
}

func (d *DemoResourceProviderClient) QueryRealServer(req *DemoResourceRsOps) (*DemoResourceRsOps, error) {
	args := d.Called(req)
	if !args.Get(0).(*DemoResourceRsOps).MockData {
		rsStatuses := make([]DemoPodStatus, 0)
		demoResourceRsStatusInProvider.Range(func(key, value any) bool {
			rsStatuses = append(rsStatuses, value.(DemoPodStatus))
			return true
		})
		return &DemoResourceRsOps{
			RsStatuses: rsStatuses,
		}, args.Error(1)
	}
	return args.Get(0).(*DemoResourceRsOps), args.Error(1)
}
