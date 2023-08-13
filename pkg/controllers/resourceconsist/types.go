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

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/ratelimiter"
)

type ReconcileAdapter interface {
	GetRateLimiter() ratelimiter.RateLimiter
	GetMaxConcurrentReconciles() int

	EmployerResource() client.Object
	EmployeeResource() client.Object
	EmployerResourceHandler() handler.EventHandler
	EmployeeResourceHandler() handler.EventHandler
	EmployerResourcePredicates() predicate.Funcs
	EmployeeResourcePredicates() predicate.Funcs

	NotFollowPodOpsLifeCycle() bool

	// GetExpectEmployerStatus and GetCurrentEmployerStatus return expect/current status of employer from related backend provider
	GetExpectEmployerStatus(ctx context.Context, employer client.Object) ([]IEmployerStatus, error)
	GetCurrentEmployerStatus(ctx context.Context, employer client.Object) ([]IEmployerStatus, error)

	CreateEmployer(employer client.Object, toCreate []IEmployerStatus) ([]IEmployerStatus, []IEmployerStatus, error)
	UpdateEmployer(employer client.Object, toUpdate []IEmployerStatus) ([]IEmployerStatus, []IEmployerStatus, error)
	DeleteEmployer(employer client.Object, toDelete []IEmployerStatus) ([]IEmployerStatus, []IEmployerStatus, error)

	RecordEmployer(succCreate, succUpdate, succDelete []IEmployerStatus) error

	// GetExpectEmployeeStatus return expect status of employees
	GetExpectEmployeeStatus(ctx context.Context, employer client.Object) ([]IEmployeeStatus, error)
	// GetCurrentEmployeeStatus return current status of employees from related backend provider
	GetCurrentEmployeeStatus(ctx context.Context, employer client.Object) ([]IEmployeeStatus, error)

	CreateEmployees(employer client.Object, toCreate []IEmployeeStatus) ([]IEmployeeStatus, []IEmployeeStatus, error)
	UpdateEmployees(employer client.Object, toUpdate []IEmployeeStatus) ([]IEmployeeStatus, []IEmployeeStatus, error)
	DeleteEmployees(employer client.Object, toDelete []IEmployeeStatus) ([]IEmployeeStatus, []IEmployeeStatus, error)
}

type IEmployerStatus interface {
	GetEmployerId() string
	GetEmployerStatuses() interface{}
	EmployerEqual(employerStatuses interface{}) (bool, error)
}

type IEmployeeStatus interface {
	GetEmployeeId() string
	GetEmployeeName() string
	GetEmployeeStatuses() interface{}
	EmployeeEqual(employeeStatus IEmployeeStatus) (bool, error)
}

type ToCUDEmployer struct {
	ToCreate  []IEmployerStatus
	ToUpdate  []IEmployerStatus
	ToDelete  []IEmployerStatus
	Unchanged []IEmployerStatus
}

type ToCUDEmployees struct {
	ToCreate  []IEmployeeStatus
	ToUpdate  []IEmployeeStatus
	ToDelete  []IEmployeeStatus
	Unchanged []IEmployeeStatus
}

type PodEmployeeStatuses struct {
	// can be set by calling SetCommonPodEmployeeStatus
	Ip             string `json:"ip,omitempty"`
	Ipv6           string `json:"ipv6,omitempty"`
	LifecycleReady bool   `json:"lifecycleReady,omitempty"`
	// extra info related to backend provider
	ExtraStatus interface{} `json:"extraStatus,omitempty"`
}
