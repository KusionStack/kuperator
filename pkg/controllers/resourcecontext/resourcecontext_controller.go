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

package resourcecontext

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	"kusionstack.io/operating/pkg/utils/mixin"
)

const (
	controllerName = "resourcecontext-controller"
)

// ResourceContextReconciler reconciles and reclaims a ResourceContext object
type ResourceContextReconciler struct {
	*mixin.ReconcilerMixin
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr ctrl.Manager) reconcile.Reconciler {
	mixin := mixin.NewReconcilerMixin(controllerName, mgr)

	InitExpectations(mixin.Client)

	return &ResourceContextReconciler{
		ReconcilerMixin: mixin,
	}
}

func AddToMgr(mgr ctrl.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{
		MaxConcurrentReconciles: 5,
		Reconciler:              r,
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1alpha1.ResourceContext{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to maintain expectation
	err = c.Watch(&source.Kind{Type: &appsv1alpha1.ResourceContext{}}, &ExpectationEventHandler{})
	if err != nil {
		return err
	}

	return nil
}

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=resourcecontexts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=resourcecontexts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=resourcecontexts/finalizers,verbs=update

// Reconcile aims to reclaim ResourceContext which is not in used which means the ResourceContext contains no Context.
func (r *ResourceContextReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("resourceContext", req.String())
	instance := &appsv1alpha1.ResourceContext{}
	if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "failed to find ResourceContext")
			return reconcile.Result{}, err
		}

		logger.Info("resourceContext is deleted")
		return ctrl.Result{}, activeExpectations.Delete(req.Namespace, req.Name)
	}

	// if expectation not satisfied, shortcut this reconciling till informer cache is updated.
	if satisfied, err := activeExpectations.IsSatisfied(instance); err != nil {
		return ctrl.Result{}, err
	} else if !satisfied {
		logger.Info("resourceContext is not satisfied to reconcile")
		return ctrl.Result{}, nil
	}

	// if ResourceContext is empty, delete it
	if len(instance.Spec.Contexts) == 0 {
		logger.Info("try to delete empty ResourceContext")
		if err := r.Client.Delete(context.TODO(), instance); err != nil {
			logger.Error(err, "failed to delete resourceContext")
			return ctrl.Result{}, err
		}
		if err := activeExpectations.ExpectDelete(instance, expectations.ResourceContext, instance.Name); err != nil {
			logger.Error(err, "failed to expect deletion after ResourceContext is deleted")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
