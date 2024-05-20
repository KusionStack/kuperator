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

package operationjob

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
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	"kusionstack.io/operating/pkg/controllers/operationjob/recreate"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	ctrlutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

const controllerName = "operationjob-controller"

var _ reconcile.Reconciler = &ReconcileOperationJob{}

// ReconcileOperationJob reconciles a OperationJob object
type ReconcileOperationJob struct {
	*mixin.ReconcilerMixin
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr manager.Manager) reconcile.Reconciler {
	reconcilerMixin := mixin.NewReconcilerMixin(controllerName, mgr)
	recreate.RegisterRecreateHandler(string(appsv1alpha1.CRRKey), &recreate.ContainerRecreateRequestHandler{})
	return &ReconcileOperationJob{
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

	// Watch for changes to OperationJob
	err = c.Watch(&source.Kind{Type: &appsv1alpha1.OperationJob{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to ContainerRecreateRequest
	err = c.Watch(&source.Kind{Type: &kruisev1alpha1.ContainerRecreateRequest{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appsv1alpha1.OperationJob{},
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

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=operationjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=operationjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=operationjobs/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile reads that state of the cluster for a OperationJob object and makes changes based on the state read
// and what is in the OperationJob.Spec
func (r *ReconcileOperationJob) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := r.Logger.WithValues("operationjob", req.String())
	instance := &appsv1alpha1.OperationJob{}
	if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	key := utils.ObjectKeyString(instance)
	if !ojutils.StatusUpToDateExpectation.SatisfiedExpectations(key, instance.ResourceVersion) {
		logger.Info("OperationJob's resourceVersion is too old, retry later", "resourceVersion.now", instance.ResourceVersion)
		return reconcile.Result{Requeue: true}, nil
	}

	if instance.DeletionTimestamp != nil {
		ojutils.StatusUpToDateExpectation.DeleteExpectations(key)
		if err := r.ReleaseTargetsForDeletion(ctx, instance, logger); err != nil {
			return reconcile.Result{}, err
		}
		// remove finalizer from operationJob
		return reconcile.Result{}, ojutils.ClearProtection(ctx, r.Client, instance)
	} else if err := ojutils.ProtectOperationJob(ctx, r.Client, instance); err != nil {
		// add finalizer on operationJob
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

	// update operationJob status
	if err = r.updateStatus(ctx, instance); err != nil {
		return reconcile.Result{}, fmt.Errorf("fail to update status of OperationJob %s: %s", req, err)
	}

	if requeueAfter != nil {
		return reconcile.Result{RequeueAfter: *requeueAfter}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileOperationJob) doReconcile(ctx context.Context,
	instance *appsv1alpha1.OperationJob,
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

	// update operationJob status
	newJobStatus, err := r.calculateStatus(instance, candidates)
	instance.Status = *newJobStatus
	return err
}

func (r *ReconcileOperationJob) calculateStatus(
	instance *appsv1alpha1.OperationJob,
	candidates []*OpsCandidate) (jobStatus *appsv1alpha1.OperationJobStatus, err error) {
	now := ctrlutils.FormatTimeNow()
	jobStatus = &appsv1alpha1.OperationJobStatus{
		StartTimestamp: instance.Status.StartTimestamp,
		EndTimestamp:   instance.Status.EndTimestamp,
		Progress:       instance.Status.Progress,
	}

	// set target ops details
	for _, candidate := range candidates {
		jobStatus.PodDetails = append(jobStatus.PodDetails, *candidate.PodOpsStatus)
	}

	// set replicas info
	var totalReplicas, completedReplicas, processingReplicas, failedReplicas int32
	for _, podDetail := range jobStatus.PodDetails {
		totalReplicas++
		if podDetail.Phase == appsv1alpha1.PodPhaseCompleted {
			completedReplicas++
		} else if podDetail.Phase == appsv1alpha1.PodPhaseFailed {
			failedReplicas++
		} else if podDetail.Phase != appsv1alpha1.PodPhaseNotStarted {
			processingReplicas++
		}
	}
	jobStatus.TotalReplicas = totalReplicas
	jobStatus.CompletedReplicas = completedReplicas
	jobStatus.ProcessingReplicas = processingReplicas
	jobStatus.FailedReplicas = failedReplicas

	// skip if ops finished
	if jobStatus.Progress == appsv1alpha1.OperationProgressCompleted ||
		jobStatus.Progress == appsv1alpha1.OperationProgressFailed {
		return
	}

	// set progress of the job
	if completedReplicas+failedReplicas == totalReplicas {
		if failedReplicas > 0 {
			jobStatus.Progress = appsv1alpha1.OperationProgressFailed
		} else {
			jobStatus.Progress = appsv1alpha1.OperationProgressCompleted
		}
	} else if totalReplicas == 0 {
		jobStatus.Progress = appsv1alpha1.OperationProgressPending
	} else {
		jobStatus.Progress = appsv1alpha1.OperationProgressProcessing
	}

	if jobStatus.EndTimestamp == nil && (completedReplicas == totalReplicas || failedReplicas == totalReplicas) {
		jobStatus.EndTimestamp = &now
	}
	return
}

func (r *ReconcileOperationJob) updateStatus(ctx context.Context, instance *appsv1alpha1.OperationJob) error {
	oldJob := &appsv1alpha1.OperationJob{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      instance.Name,
	}, oldJob); err != nil {
		return err
	}

	newJobStatus := instance.Status
	if equality.Semantic.DeepEqual(oldJob.Status, newJobStatus) {
		return nil
	}

	err := ojutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(instance), instance.ResourceVersion)
	if err != nil {
		return err
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		oldJob.Status = newJobStatus
		return r.Client.Status().Update(ctx, oldJob)
	})
	if err != nil {
		ojutils.StatusUpToDateExpectation.DeleteExpectations(utils.ObjectKeyString(instance))
	}
	return err
}
