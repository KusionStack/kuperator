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

package collaset

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/kafed/pkg/controllers/collaset/synccontrol"
	"kusionstack.io/kafed/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/kafed/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/kafed/pkg/controllers/utils"
	"kusionstack.io/kafed/pkg/controllers/utils/expectations"
	"kusionstack.io/kafed/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/kafed/pkg/controllers/utils/revision"
)

const (
	controllerName = "collaset-controller"
)

var (
	revisionManager *revision.RevisionManager
	podControl      podcontrol.Interface
	syncControl     synccontrol.Interface
)

// CollaSetReconciler reconciles a CollaSet object
type CollaSetReconciler struct {
	client.Client

	recorder record.EventRecorder
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr ctrl.Manager) reconcile.Reconciler {
	recorder := mgr.GetEventRecorderFor(controllerName)

	revisionManager = revision.NewRevisionManager(mgr.GetClient(), mgr.GetScheme(), &revisionOwnerAdapter{})
	podControl = podcontrol.NewRealPodControl(mgr.GetClient(), mgr.GetScheme())
	syncControl = synccontrol.NewRealSyncControl(mgr.GetClient(), podControl, recorder)

	collasetutils.InitExpectations(mgr.GetClient())

	return &CollaSetReconciler{
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

	err = c.Watch(&source.Kind{Type: &appsv1alpha1.CollaSet{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appsv1alpha1.CollaSet{},
	})
	if err != nil {
		return err
	}

	return nil
}

//+kubebuilder:rbac:groups=apps.kafed.io,resources=collasets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.kafed.io,resources=collasets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.kafed.io,resources=collasets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *CollaSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	instance := &appsv1alpha1.CollaSet{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if !errors.IsNotFound(err) {
			klog.Error("fail to find CollaSet %s: %s", req, err)
			return reconcile.Result{}, err
		}

		klog.Infof("CollaSet %s is deleted", req)
		return ctrl.Result{}, collasetutils.ActiveExpectations.Delete(req.Namespace, req.Name)
	}

	// if expectation not satisfied, shortcut this reconciling till informer cache is updated.
	if satisfied, err := collasetutils.ActiveExpectations.IsSatisfied(instance); err != nil {
		return ctrl.Result{}, err
	} else if !satisfied {
		klog.Warningf("CollaSet %s is not satisfied to reconcile.", req)
		return ctrl.Result{}, nil
	}

	newStatus, err := DoReconcile(instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateStatus(ctx, instance, newStatus); err != nil {
		return ctrl.Result{}, fmt.Errorf("fail to update status of CollaSet %s: %s", req, err)
	}

	return ctrl.Result{}, nil
}

func DoReconcile(instance *appsv1alpha1.CollaSet) (*appsv1alpha1.CollaSetStatus, error) {
	newStatus := instance.Status.DeepCopy()

	currentRevision, updatedRevision, revisions, collisionCount, _, err := revisionManager.ConstructRevisions(instance, false)
	if err != nil {
		return newStatus, fmt.Errorf("fail to construct revision for CollaSet %s/%s: %s", instance.Namespace, instance.Name, err)
	}

	// record collisionCount
	newStatus.CollisionCount = collisionCount
	newStatus.CurrentRevision = currentRevision.Name
	newStatus.UpdatedRevision = updatedRevision.Name

	podWrappers, newStatus, syncErr := doSync(instance, updatedRevision, revisions, newStatus)

	newStatus = calculateStatus(instance, newStatus, updatedRevision, podWrappers, syncErr)
	if syncErr != nil {
		return newStatus, syncErr
	}

	return newStatus, nil
}

func doSync(instance *appsv1alpha1.CollaSet, updatedRevision *appsv1.ControllerRevision, revisions []*appsv1.ControllerRevision, newStatus *appsv1alpha1.CollaSetStatus) ([]*collasetutils.PodWrapper, *appsv1alpha1.CollaSetStatus, error) {
	filteredPods, err := podControl.GetFilteredPods(&instance.Spec.Selector, instance)
	if err != nil {
		return nil, newStatus, err
	}

	synced, podWrappers, ownedIDs, err := syncControl.SyncPods(instance, filteredPods, updatedRevision, newStatus)
	if err != nil {
		return podWrappers, newStatus, err
	} else if synced {
		return podWrappers, newStatus, nil
	}

	if scaling, err := syncControl.Scale(instance, podWrappers, revisions, updatedRevision, ownedIDs, newStatus); err != nil {
		return podWrappers, newStatus, err
	} else if scaling {
		return podWrappers, newStatus, nil
	}

	if _, err := syncControl.Update(instance, podWrappers, revisions, updatedRevision, ownedIDs, newStatus); err != nil {
		return podWrappers, newStatus, err
	}

	return podWrappers, newStatus, nil
}

func calculateStatus(instance *appsv1alpha1.CollaSet, newStatus *appsv1alpha1.CollaSetStatus, updatedRevision *appsv1.ControllerRevision, podWrappers []*collasetutils.PodWrapper, syncErr error) *appsv1alpha1.CollaSetStatus {
	if syncErr == nil {
		newStatus.ObservedGeneration = instance.Generation
	}

	var scheduledReplicas, readyReplicas, availableReplicas, replicas, updatedReplicas, operatingReplicas,
		updatedReadyReplicas, updatedAvailableReplicas int32

	for _, podWrapper := range podWrappers {
		replicas++

		isUpdated := false
		if isUpdated = controllerutils.IsPodUpdatedRevision(podWrapper.Pod, updatedRevision.Name); isUpdated {
			updatedReplicas++
		}

		if podopslifecycle.IsDuringOps(utils.UpdateOpsLifecycleAdapter, podWrapper) || podopslifecycle.IsDuringOps(utils.ScaleInOpsLifecycleAdapter, podWrapper) {
			operatingReplicas++
		}

		if controllerutils.IsPodScheduled(podWrapper.Pod) {
			scheduledReplicas++
		}

		if controllerutils.IsPodReady(podWrapper.Pod) {
			readyReplicas++
			if isUpdated {
				updatedReadyReplicas++
			}
		}

		if controllerutils.IsServiceAvailable(podWrapper.Pod) {
			availableReplicas++
			if isUpdated {
				updatedAvailableReplicas++
			}
		}
	}

	newStatus.ScheduledReplicas = scheduledReplicas
	newStatus.ReadyReplicas = readyReplicas
	newStatus.AvailableReplicas = availableReplicas
	newStatus.Replicas = replicas
	newStatus.UpdatedReplicas = updatedReplicas
	newStatus.OperatingReplicas = operatingReplicas
	newStatus.UpdatedReadyReplicas = updatedReadyReplicas
	newStatus.UpdatedAvailableReplicas = updatedAvailableReplicas

	if newStatus.UpdatedAvailableReplicas == instance.Spec.Replicas {
		newStatus.CurrentRevision = updatedRevision.Name
	}

	return newStatus
}

func (r *CollaSetReconciler) updateStatus(ctx context.Context, instance *appsv1alpha1.CollaSet, newStatus *appsv1alpha1.CollaSetStatus) error {
	if equality.Semantic.DeepEqual(instance.Status, newStatus) {
		return nil
	}

	instance.Status = *newStatus

	err := r.Status().Update(ctx, instance)
	if err == nil {
		if err := collasetutils.ActiveExpectations.ExpectUpdate(instance, expectations.CollaSet, instance.Name, instance.ResourceVersion); err != nil {
			return err
		}
	}

	return err
}
