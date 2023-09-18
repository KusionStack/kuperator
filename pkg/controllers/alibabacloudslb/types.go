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

package alibabacloudslb

import (
	"fmt"
	"reflect"

	"kusionstack.io/operating/pkg/controllers/resourceconsist"
)

var _ resourceconsist.IEmployee = AlibabaSlbPodStatus{}

type AlibabaSlbPodStatus struct {
	EmployeeID       string
	EmployeeName     string
	EmployeeStatuses resourceconsist.PodEmployeeStatuses
}

type PodExtraStatus struct {
	TrafficOn bool
}

func (a AlibabaSlbPodStatus) GetEmployeeId() string {
	return a.EmployeeID
}

func (a AlibabaSlbPodStatus) GetEmployeeName() string {
	return a.EmployeeName
}

func (a AlibabaSlbPodStatus) GetEmployeeStatuses() interface{} {
	return a.EmployeeStatuses
}

func (a AlibabaSlbPodStatus) EmployeeEqual(employeeStatus resourceconsist.IEmployee) (bool, error) {
	if a.EmployeeName != employeeStatus.GetEmployeeName() {
		return false, nil
	}

	podEmployeeStatuses, ok := employeeStatus.GetEmployeeStatuses().(resourceconsist.PodEmployeeStatuses)
	if !ok {
		return false, fmt.Errorf("employee to diff is not Pod Employee status")
	}

	if a.EmployeeStatuses.Ip != podEmployeeStatuses.Ip || a.EmployeeStatuses.LifecycleReady != podEmployeeStatuses.LifecycleReady ||
		a.EmployeeStatuses.Ipv6 != podEmployeeStatuses.Ipv6 {
		return false, nil
	}

	return reflect.DeepEqual(a.EmployeeStatuses.ExtraStatus, podEmployeeStatuses.ExtraStatus), nil
}
