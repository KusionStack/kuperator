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
	recreate.RegisterRecreateHandler(recreate.KruiseCcontainerRecreateRequest, &recreate.ContainerRecreateRequestHandler{})
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
		if err := r.ReleaseTargetsForDeletion(ctx, instance, logger); err != nil {
			return reconcile.Result{}, err
		}
		ojutils.StatusUpToDateExpectation.DeleteExpectations(key)
		return reconcile.Result{}, ojutils.ClearProtection(ctx, r.Client, instance)
	} else if err := ojutils.ProtectOperationJob(ctx, r.Client, instance); err != nil {
		// add finalizer on operationJob
		return reconcile.Result{}, err
	}

	if err := r.doReconcile(ctx, instance, logger); err != nil {
		return reconcile.Result{}, err
	}

	// handle timeout and TTL
	jobDeleted, requeueAfter, err := r.ensureActiveDeadlineOrTTL(ctx, instance, logger)
	if jobDeleted || err != nil {
		return reconcile.Result{}, err
	}

	if err := r.updateStatus(ctx, instance); err != nil {
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
	candidates, err := operator.ListTargets()
	if err != nil {
		return err
	}

	filteredCandidates := DecideCandidateByPartition(instance, candidates)
	for _, candidate := range filteredCandidates {
		if err = operator.OperateTarget(candidate); err != nil {
			return err
		}
		if err = operator.FulfilPodOpsStatus(candidate); err != nil {
			return err
		}
	}

	instance.Status = r.calculateStatus(instance, candidates)
	return nil
}

func (r *ReconcileOperationJob) calculateStatus(
	instance *appsv1alpha1.OperationJob,
	candidates []*OpsCandidate) (jobStatus appsv1alpha1.OperationJobStatus) {
	now := ctrlutils.FormatTimeNow()
	jobStatus = appsv1alpha1.OperationJobStatus{
		StartTimestamp:     instance.Status.StartTimestamp,
		EndTimestamp:       instance.Status.EndTimestamp,
		Progress:           instance.Status.Progress,
		ObservedGeneration: instance.Generation,
	}

	for _, candidate := range candidates {
		jobStatus.PodDetails = append(jobStatus.PodDetails, *candidate.PodOpsStatus)
	}

	var totalPodCount, succeededPodCount, failedPodCount, pendingPodCount int32
	for _, podDetail := range jobStatus.PodDetails {
		totalPodCount++

		if podDetail.Progress == appsv1alpha1.OperationProgressFailed {
			failedPodCount++
		}

		if podDetail.Progress == appsv1alpha1.OperationProgressSucceeded {
			succeededPodCount++
		}

		if podDetail.Progress == appsv1alpha1.OperationProgressPending {
			pendingPodCount++
		}

	}

	if !ojutils.IsJobFinished(&appsv1alpha1.OperationJob{Status: jobStatus}) {
		jobStatus.Progress = appsv1alpha1.OperationProgressProcessing

		if pendingPodCount == totalPodCount {
			jobStatus.Progress = appsv1alpha1.OperationProgressPending
		}

		if succeededPodCount+failedPodCount == totalPodCount {
			if failedPodCount > 0 {
				jobStatus.Progress = appsv1alpha1.OperationProgressFailed
			} else {
				jobStatus.Progress = appsv1alpha1.OperationProgressSucceeded
			}

			if jobStatus.EndTimestamp == nil {
				jobStatus.EndTimestamp = &now
			}
		}
	}

	jobStatus.TotalPodCount = totalPodCount
	jobStatus.FailedPodCount = failedPodCount
	jobStatus.SucceededPodCount = succeededPodCount
	return
}

func (r *ReconcileOperationJob) updateStatus(ctx context.Context, newJob *appsv1alpha1.OperationJob) error {
	oldJob := &appsv1alpha1.OperationJob{}
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: newJob.Namespace, Name: newJob.Name}, oldJob); err != nil {
		return err
	}

	if equality.Semantic.DeepEqual(oldJob.Status, newJob.Status) {
		return nil
	}

	if err := ojutils.StatusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(newJob), newJob.ResourceVersion); err != nil {
		return err
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		oldJob.Status = newJob.Status
		return r.Client.Status().Update(ctx, oldJob)
	}); err != nil {
		ojutils.StatusUpToDateExpectation.DeleteExpectations(utils.ObjectKeyString(newJob))
		return err
	}

	return nil
}
