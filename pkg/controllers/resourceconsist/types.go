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

// ReconcileOptions includes max concurrent reconciles and rate limiter,
// max concurrent reconcile: 5 and DefaultControllerRateLimiter() will be used if ReconcileOptions not implemented.
type ReconcileOptions interface {
	GetRateLimiter() ratelimiter.RateLimiter
	GetMaxConcurrentReconciles() int
}

// ReconcileWatchOptions defines what employer and employee is and how controller watch
// default employer: Service, default employee: Pod
type ReconcileWatchOptions interface {
	NewEmployer() client.Object
	NewEmployee() client.Object
	EmployerEventHandler() handler.EventHandler
	EmployeeEventHandler() handler.EventHandler
	EmployerPredicates() predicate.Funcs
	EmployeePredicates() predicate.Funcs
}

// ReconcileAdapter is the interface that customized controllers should implement.
type ReconcileAdapter interface {
	GetControllerName() string
	NotFollowPodOpsLifeCycle() bool

	// GetExpectEmployer and GetCurrentEmployer return expect/current status of employer from related backend provider
	GetExpectEmployer(ctx context.Context, employer client.Object) ([]IEmployer, error)
	GetCurrentEmployer(ctx context.Context, employer client.Object) ([]IEmployer, error)

	CreateEmployer(employer client.Object, toCreate []IEmployer) ([]IEmployer, []IEmployer, error)
	UpdateEmployer(employer client.Object, toUpdate []IEmployer) ([]IEmployer, []IEmployer, error)
	DeleteEmployer(employer client.Object, toDelete []IEmployer) ([]IEmployer, []IEmployer, error)

	RecordEmployer(succCreate, succUpdate, succDelete []IEmployer) error

	// GetExpectEmployee return expect status of employees
	GetExpectEmployee(ctx context.Context, employer client.Object) ([]IEmployee, error)
	// GetCurrentEmployee return current status of employees from related backend provider
	GetCurrentEmployee(ctx context.Context, employer client.Object) ([]IEmployee, error)

	CreateEmployees(employer client.Object, toCreate []IEmployee) ([]IEmployee, []IEmployee, error)
	UpdateEmployees(employer client.Object, toUpdate []IEmployee) ([]IEmployee, []IEmployee, error)
	DeleteEmployees(employer client.Object, toDelete []IEmployee) ([]IEmployee, []IEmployee, error)
}

type IEmployer interface {
	GetEmployerId() string
	GetEmployerStatuses() interface{}
	EmployerEqual(employerStatuses interface{}) (bool, error)
}

type IEmployee interface {
	GetEmployeeId() string
	GetEmployeeName() string
	GetEmployeeStatuses() interface{}
	EmployeeEqual(employee IEmployee) (bool, error)
}

type ToCUDEmployer struct {
	ToCreate  []IEmployer
	ToUpdate  []IEmployer
	ToDelete  []IEmployer
	Unchanged []IEmployer
}

type ToCUDEmployees struct {
	ToCreate  []IEmployee
	ToUpdate  []IEmployee
	ToDelete  []IEmployee
	Unchanged []IEmployee
}

type PodEmployeeStatuses struct {
	// can be set by calling SetCommonPodEmployeeStatus
	Ip             string `json:"ip,omitempty"`
	Ipv6           string `json:"ipv6,omitempty"`
	LifecycleReady bool   `json:"lifecycleReady,omitempty"`
	// extra info related to backend provider
	ExtraStatus interface{} `json:"extraStatus,omitempty"`
}
