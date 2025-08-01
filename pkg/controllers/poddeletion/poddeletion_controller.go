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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kusionstack.io/kube-api/apps/v1alpha1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/collaset/pvccontrol"
	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
	"kusionstack.io/kuperator/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

const (
	controllerName = "poddeletion-controller"
)

// PodDeletionReconciler reconciles and reclaims a Pod object
type PodDeletionReconciler struct {
	*mixin.ReconcilerMixin
	pvcControl pvccontrol.Interface
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr ctrl.Manager) reconcile.Reconciler {
	mixin := mixin.NewReconcilerMixin(controllerName, mgr)

	InitExpectations(mixin.Client)

	return &PodDeletionReconciler{
		ReconcilerMixin: mixin,
		pvcControl:      pvccontrol.NewRealPvcControl(mixin.Client, mixin.Scheme),
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

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile aims to delete Pod through PodOpsLifecycle. It will watch Pod with deleting indication label.
// If a Pod is labeled, controller will first trigger a deletion PodOpsLifecycle. If all conditions are satisfied,
// it will then delete Pod.
func (r *PodDeletionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("pod", req.String())
	instance := &corev1.Pod{}
	if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "failed to find pod")
			return reconcile.Result{}, err
		}

		logger.Info("pod is deleted")
		return ctrl.Result{}, activeExpectations.Delete(req.Namespace, req.Name)
	}

	// if expectation not satisfied, shortcut this reconciling till informer cache is updated.
	if satisfied, err := activeExpectations.IsSatisfied(instance); err != nil {
		return ctrl.Result{}, err
	} else if !satisfied {
		logger.Info("pod is not satisfied to reconcile")
		return ctrl.Result{}, nil
	}

	if instance.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}

	// if Pod is not begin a deletion PodOpsLifecycle, trigger it
	if !podopslifecycle.IsDuringOps(OpsLifecycleAdapter, instance) {
		if updated, err := podopslifecycle.Begin(r.Client, OpsLifecycleAdapter, instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("fail to begin PodOpsLifecycle to delete Pod %s: %s", req, err)
		} else if updated {
			if err := activeExpectations.ExpectUpdate(instance, expectations.Pod, instance.Name, instance.ResourceVersion); err != nil {
				return ctrl.Result{}, fmt.Errorf("fail to expect Pod updated after beginning PodOpsLifecycle to delete Pod %s: %s", req, err)
			}
		}
	}

	// if Pod is allowed to operate, delete it
	if _, allowed := podopslifecycle.AllowOps(OpsLifecycleAdapter, 0, instance); allowed {
		logger.Info("try to delete Pod with deletion indication")
		if err := r.Client.Delete(context.TODO(), instance); err != nil {
			return ctrl.Result{}, fmt.Errorf("fail to delete Pod %s with deletion indication: %s", req, err)
		} else {
			r.Recorder.Event(instance, corev1.EventTypeNormal, v1alpha1.PodDeletionEvent, "Deleted pod")

			if err := activeExpectations.ExpectDelete(instance, expectations.Pod, instance.Name); err != nil {
				return ctrl.Result{}, fmt.Errorf("fail to expect Pod %s to be deleted: %s", req, err)
			}
		}
		// if this pod in replaced update, delete pvcs
		_, isReplaceOriginPod := instance.Labels[appsv1alpha1.PodReplaceIndicationLabelKey]
		_, isReplaceNewPod := instance.Labels[appsv1alpha1.PodReplacePairOriginName]
		if isReplaceOriginPod || isReplaceNewPod {
			err := r.deleteReplacedPodPvcs(ctx, instance)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *PodDeletionReconciler) deleteReplacedPodPvcs(ctx context.Context, pod *corev1.Pod) error {
	ownerRefs := pod.GetOwnerReferences()
	cls := &appsv1alpha1.CollaSet{}
	for _, ref := range ownerRefs {
		if ref.Kind == "CollaSet" {
			if err := r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: ref.Name}, cls); err != nil {
				return err
			}
			break
		}
	}
	if cls.Spec.VolumeClaimTemplates == nil {
		return nil
	}
	pvcs, err := r.pvcControl.GetFilteredPvcs(ctx, cls)
	if err != nil {
		return err
	}
	return r.pvcControl.DeletePodPvcs(ctx, cls, pod, pvcs)
}
