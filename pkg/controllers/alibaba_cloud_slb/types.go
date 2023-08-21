package alibaba_cloud_slb

import (
	"fmt"
	"kusionstack.io/kafed/pkg/controllers/resourceconsist"
	"reflect"
)

var _ resourceconsist.IEmployeeStatus = AlibabaSlbPodStatus{}

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

func (a AlibabaSlbPodStatus) EmployeeEqual(employeeStatus resourceconsist.IEmployeeStatus) (bool, error) {
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
