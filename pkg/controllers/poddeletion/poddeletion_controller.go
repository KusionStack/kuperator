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

package poddeletion

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"kusionstack.io/kafed/pkg/controllers/utils/expectations"
	"kusionstack.io/kafed/pkg/controllers/utils/podopslifecycle"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kusionstack.io/kafed/pkg/controllers/collaset/utils"
)

const (
	controllerName = "poddeletion-controller"
)

// PodDeletionReconciler reconciles and reclaims a Pod object
type PodDeletionReconciler struct {
	client.Client

	recorder record.EventRecorder
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr ctrl.Manager) reconcile.Reconciler {
	recorder := mgr.GetEventRecorderFor(controllerName)

	InitExpectations(mgr.GetClient())

	return &PodDeletionReconciler{
		Client:   mgr.GetClient(),
		recorder: recorder,
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

	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}, &PredicateDeletionIndicatedPod{})
	if err != nil {
		return err
	}

	return nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PodDeletionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	instance := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if !errors.IsNotFound(err) {
			klog.Error("fail to find Pod %s: %s", req, err)
			utils.ActiveExpectations.Delete(req.Namespace, req.Name)
			return reconcile.Result{}, err
		}

		klog.Infof("Pod %s is deleted", req)
		return ctrl.Result{}, nil
	}

	// if expectation not satisfied, shortcut this reconciling till informer cache is updated.
	if satisfied, err := activeExpectations.IsSatisfied(instance); err != nil {
		return ctrl.Result{}, err
	} else if !satisfied {
		klog.Warningf("Pod %s is not satisfied to reconcile.", req)
		return ctrl.Result{}, nil
	}

	if instance.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// if Pod is not begin a deletion PodOpsLifecycle, trigger it
	if !podopslifecycle.IsDuringOps(OpsLifecycleAdapter, instance) {
		if updated, err := podopslifecycle.Begin(r, OpsLifecycleAdapter, instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("fail to begin PodOpsLifecycle to delete Pod %s: %s", req, err)
		} else if updated {
			if err := activeExpectations.ExpectUpdate(instance, expectations.Pod, instance.Name, instance.ResourceVersion); err != nil {
				return ctrl.Result{}, fmt.Errorf("fail to expect Pod updated after beginning PodOpsLifecycle to delete Pod %s: %s", req, err)
			}
		}
	}

	// if Pod is allow to operate, delete it
	if podopslifecycle.AllowOps(OpsLifecycleAdapter, instance) {
		klog.Infof("try to delete Pod %s with deletion indication", req)
		if err := r.Delete(context.TODO(), instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("fail to delete Pod %s with deletion indication: %s", req, err)
		} else {
			if err := activeExpectations.ExpectDelete(instance, expectations.Pod, instance.Name); err != nil {
				return ctrl.Result{}, fmt.Errorf("fail to expect Pod  %s deleted: %s", req, err)
			}
		}
	}

	return ctrl.Result{}, nil
}
