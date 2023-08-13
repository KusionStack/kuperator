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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kusionstack.io/kafed/pkg/log"
)

// Add creates a new Controller of specified reconcileAdapter and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager, reconcileAdapter ReconcileAdapter) error {
	return AddToMgr(mgr, reconcileAdapter)
}

func AddToMgr(mgr manager.Manager, adapter ReconcileAdapter) error {
	r := NewReconcile(mgr, adapter)

	// CreateEmployees a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{
		MaxConcurrentReconciles: adapter.GetMaxConcurrentReconciles(),
		Reconciler:              r,
		RateLimiter:             adapter.GetRateLimiter()})
	if err != nil {
		return err
	}

	// Watch for changes to EmployerResources
	err = c.Watch(&source.Kind{
		Type: adapter.EmployerResource()},
		adapter.EmployerResourceHandler(),
		adapter.EmployerResourcePredicates())
	if err != nil {
		return err
	}

	// Watch for changes to EmployeeResources
	err = c.Watch(&source.Kind{
		Type: adapter.EmployeeResource()},
		adapter.EmployeeResourceHandler(),
		adapter.EmployeeResourcePredicates())
	if err != nil {
		return err
	}

	return nil
}

func NewReconcile(mgr manager.Manager, reconcileAdapter ReconcileAdapter) *Consist {
	return &Consist{
		Client:  mgr.GetClient(),
		scheme:  mgr.GetScheme(),
		adapter: reconcileAdapter,
		logger:  log.New(logf.Log.WithName("resourceconsist-controller")),
	}
}

type Consist struct {
	client.Client
	scheme  *runtime.Scheme
	logger  *log.Logger
	adapter ReconcileAdapter
}

func (r *Consist) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	employer := r.adapter.EmployerResource()
	err := r.Get(ctx, types.NamespacedName{
		Namespace: request.Namespace,
		Name:      request.Name,
	}, employer)
	if err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		r.logger.Errorf("get employer failed: %s", err.Error())
		return reconcile.Result{}, err
	}

	// ensure employer-clean finalizer firstly, employer-clean finalizer should be cleaned at the end
	err = r.ensureEmployerCleanFlzFirstAdd(ctx, employer)
	if err != nil {
		r.logger.Errorf("add employer clean finalizer failed: %s", err.Error())
		return reconcile.Result{}, err
	}

	expectEmployerStatus, err := r.adapter.GetExpectEmployerStatus(ctx, employer)
	if err != nil {
		r.logger.Errorf("get expect employer status failed: %s", err.Error())
		return reconcile.Result{}, err
	}
	currentEmployerStatus, err := r.adapter.GetCurrentEmployerStatus(ctx, employer)
	if err != nil {
		r.logger.Errorf("get current employer status failed: %s", err.Error())
		return reconcile.Result{}, err
	}
	isCleanEmployer, err := r.syncEmployer(ctx, employer, expectEmployerStatus, currentEmployerStatus)
	if err != nil {
		r.logger.Errorf("sync employer status failed: %s", err.Error())
		return reconcile.Result{}, err
	}

	expectEmployees, err := r.adapter.GetExpectEmployeeStatus(ctx, employer)
	if err != nil {
		r.logger.Errorf("get expect employees status failed: %s", err.Error())
		return reconcile.Result{}, err
	}
	currentEmployees, err := r.adapter.GetCurrentEmployeeStatus(ctx, employer)
	if err != nil {
		r.logger.Errorf("get current employees status failed: %s", err.Error())
		return reconcile.Result{}, err
	}
	isCleanEmployee, err := r.syncEmployees(ctx, employer, expectEmployees, currentEmployees)
	if err != nil {
		r.logger.Errorf("sync employees status failed: %s", err.Error())
		return reconcile.Result{}, err
	}

	if isCleanEmployer && isCleanEmployee && !employer.GetDeletionTimestamp().IsZero() {
		err = r.cleanEmployerCleanFinalizer(ctx, employer)
		if err != nil {
			r.logger.Errorf("clean employer clean-finalizer failed: %s", err.Error())
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}
