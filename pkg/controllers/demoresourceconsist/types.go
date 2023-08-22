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
	"fmt"
	"reflect"

	"kusionstack.io/kafed/pkg/controllers/resourceconsist"
)

var _ resourceconsist.IEmployer = DemoServiceStatus{}
var _ resourceconsist.IEmployee = DemoPodStatus{}

type DemoServiceStatus struct {
	EmployerId       string
	EmployerStatuses interface{}
}

func (d DemoServiceStatus) GetEmployerId() string {
	return d.EmployerId
}

func (d DemoServiceStatus) GetEmployerStatuses() interface{} {
	return d.EmployerStatuses
}

func (d DemoServiceStatus) EmployerEqual(employerStatuses interface{}) (bool, error) {
	return true, nil
}

type DemoPodStatus struct {
	EmployeeId       string
	EmployeeName     string
	EmployeeStatuses resourceconsist.PodEmployeeStatuses
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

func (d DemoPodStatus) EmployeeEqual(employeeStatus resourceconsist.IEmployee) (bool, error) {
	if d.EmployeeName != employeeStatus.GetEmployeeName() {
		return false, nil
	}

	podEmployeeStatuses, ok := employeeStatus.GetEmployeeStatuses().(resourceconsist.PodEmployeeStatuses)
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
