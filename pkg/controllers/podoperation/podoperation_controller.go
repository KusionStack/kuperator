/*
Copyright 2024 The KusionStack Authors.

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

package podoperation

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/podoperation/opscontrol"
	"kusionstack.io/operating/pkg/controllers/podoperation/recreate"
	podoperationutils "kusionstack.io/operating/pkg/controllers/podoperation/utils"
	ctrlutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

const controllerName = "podoperation-controller"

var _ reconcile.Reconciler = &ReconcilePodOperation{}

// ReconcilePodOperation reconciles a PodOperation object
type ReconcilePodOperation struct {
	*mixin.ReconcilerMixin
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconcilerMixin := mixin.NewReconcilerMixin(controllerName, mgr)
	recreate.RegisterRecreateHandler(string(appsv1alpha1.CRRKey), &recreate.ContainerRecreateRequestHandler{})
	return &ReconcilePodOperation{
		ReconcilerMixin: reconcilerMixin,
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

	// Watch for changes to PodOperation
	err = c.Watch(&source.Kind{Type: &appsv1alpha1.PodOperation{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to ContainerRecreateRequest
	err = c.Watch(&source.Kind{Type: &kruisev1alpha1.ContainerRecreateRequest{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appsv1alpha1.PodOperation{},
	})
	if err != nil {
		return err
	}

	// Watch for changes to Pod
	managerClient := mgr.GetClient()
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &PodHandler{Client: managerClient})
	if err != nil {
		return err
	}

	return nil
}

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=podoperations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=podoperations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=podoperations/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile reads that state of the cluster for a PodOperation object and makes changes based on the state read
// and what is in the PodOperation.Spec
func (r *ReconcilePodOperation) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.Logger.WithValues("podoperation", req.String())
	instance := &appsv1alpha1.PodOperation{}
	if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	key := utils.ObjectKeyString(instance)
	if !podoperationutils.StatusUpToDateExpectation.SatisfiedExpectations(key, instance.ResourceVersion) {
		logger.Info("PodOperation's resourceVersion is too old, retry later", "resourceVersion.now", instance.ResourceVersion)
		return reconcile.Result{Requeue: true}, nil
	}

	if instance.DeletionTimestamp != nil {
		if err := r.ReleaseTargetsForDeletion(ctx, instance, logger); err != nil {
			return reconcile.Result{}, err
		}
		podoperationutils.StatusUpToDateExpectation.DeleteExpectations(key)
		// remove finalizer from podOperation
		return reconcile.Result{}, podoperationutils.ClearProtection(ctx, r.Client, instance)
	} else if err := podoperationutils.ProtectPodOperation(ctx, r.Client, instance); err != nil {
		// add finalizer on podOperation
		return reconcile.Result{}, err
	}

	// operate targets
	err := r.doReconcile(ctx, instance, logger)
	if err != nil {
		return reconcile.Result{}, err
	}

	// handle timeout and TTL
	cleaned, requeueAfter, err := r.ensureActiveDeadlineOrTTL(ctx, instance, logger)
	if cleaned || err != nil {
		return reconcile.Result{}, err
	}

	// update podOperation status
	if err = r.updateStatus(ctx, instance); err != nil {
		return reconcile.Result{}, fmt.Errorf("fail to update status of PodOperation %s: %s", req, err)
	}

	if requeueAfter != nil {
		return reconcile.Result{RequeueAfter: *requeueAfter}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcilePodOperation) doReconcile(ctx context.Context,
	instance *appsv1alpha1.PodOperation,
	logger logr.Logger) error {
	operator := r.newOperator(ctx, instance, logger)

	// list operate targets
	candidates, err := operator.ListTargets()
	if err != nil {
		return err
	}

	// operate targets and fulfil podOpsStatus
	filteredCandidates := DecideCandidateByPartition(instance, candidates)
	for _, candidate := range filteredCandidates {
		if err = operator.OperateTarget(candidate); err != nil {
			return err
		}
		if err = operator.FulfilPodOpsStatus(candidate); err != nil {
			return err
		}
	}

	// update podOperation status
	newStatus, err := r.calculateStatus(instance, candidates)
	instance.Status = *newStatus
	return err
}

func (r *ReconcilePodOperation) calculateStatus(
	instance *appsv1alpha1.PodOperation,
	candidates []*OpsCandidate) (newStatus *appsv1alpha1.PodOperationStatus, err error) {
	now := ctrlutils.FormatTimeNow()
	newStatus = &appsv1alpha1.PodOperationStatus{
		StartTimestamp: instance.Status.StartTimestamp,
		EndTimestamp:   instance.Status.EndTimestamp,
		Progress:       instance.Status.Progress,
	}

	// set target ops details
	for _, candidate := range candidates {
		newStatus.PodDetails = append(newStatus.PodDetails, *candidate.PodOpsStatus)
	}

	// set replicas info
	var totalReplicas, completedReplicas, processingReplicas, failedReplicas int32
	for _, podDetail := range newStatus.PodDetails {
		totalReplicas++
		if podDetail.Phase == appsv1alpha1.PodPhaseCompleted {
			completedReplicas++
		} else if podDetail.Phase == appsv1alpha1.PodPhaseFailed {
			failedReplicas++
		} else if podDetail.Phase != appsv1alpha1.PodPhaseNotStarted {
			processingReplicas++
		}
	}
	newStatus.TotalReplicas = totalReplicas
	newStatus.CompletedReplicas = completedReplicas
	newStatus.ProcessingReplicas = processingReplicas
	newStatus.FailedReplicas = failedReplicas

	// skip if ops finished
	if newStatus.Progress == appsv1alpha1.OperationProgressCompleted ||
		newStatus.Progress == appsv1alpha1.OperationProgressFailed {
		return
	}

	// set progress of the new
	if completedReplicas+failedReplicas == totalReplicas {
		if failedReplicas > 0 {
			newStatus.Progress = appsv1alpha1.OperationProgressFailed
		} else {
			newStatus.Progress = appsv1alpha1.OperationProgressCompleted
		}
	} else if totalReplicas == 0 {
		newStatus.Progress = appsv1alpha1.OperationProgressPending
	} else {
		newStatus.Progress = appsv1alpha1.OperationProgressProcessing
	}

	if newStatus.EndTimestamp == nil && (completedReplicas == totalReplicas || failedReplicas == totalReplicas) {
		newStatus.EndTimestamp = &now
	}
	return
}

func (r *ReconcilePodOperation) updateStatus(ctx context.Context, instance *appsv1alpha1.PodOperation) error {
	oldObj := &appsv1alpha1.PodOperation{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      instance.Name,
	}, oldObj); err != nil {
		return err
	}

	newStatus := instance.Status
	if equality.Semantic.DeepEqual(oldObj.Status, newStatus) {
		return nil
	}

	err := podoperationutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(instance), instance.ResourceVersion)
	if err != nil {
		return err
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		oldObj.Status = newStatus
		return r.Client.Status().Update(ctx, oldObj)
	})
	if err != nil {
		podoperationutils.StatusUpToDateExpectation.DeleteExpectations(utils.ObjectKeyString(instance))
	}
	return err
}
