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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontext"
	"kusionstack.io/operating/pkg/controllers/collaset/podcontrol"
	"kusionstack.io/operating/pkg/controllers/collaset/pvccontrol"
	"kusionstack.io/operating/pkg/controllers/collaset/synccontrol"
	"kusionstack.io/operating/pkg/controllers/collaset/utils"
	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/controllers/utils/poddecoration/strategy"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
	"kusionstack.io/operating/pkg/controllers/utils/revision"
	commonutils "kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

const (
	controllerName = "collaset-controller"

	preReclaimFinalizer = "apps.kusionstack.io/pre-reclaim"
)

// CollaSetReconciler reconciles a CollaSet object
type CollaSetReconciler struct {
	*mixin.ReconcilerMixin

	revisionManager *revision.RevisionManager
	syncControl     synccontrol.Interface
}

func Add(mgr ctrl.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

// NewReconciler returns a new reconcile.Reconciler
func NewReconciler(mgr ctrl.Manager) reconcile.Reconciler {
	mixin := mixin.NewReconcilerMixin(controllerName, mgr)
	collasetutils.InitExpectations(mixin.Client)

	return &CollaSetReconciler{
		ReconcilerMixin: mixin,
		revisionManager: revision.NewRevisionManager(mixin.Client, mixin.Scheme, NewRevisionOwnerAdapter(podcontrol.NewRealPodControl(mixin.Client, mixin.Scheme))),
		syncControl:     synccontrol.NewRealSyncControl(mixin.Client, mixin.Logger, podcontrol.NewRealPodControl(mixin.Client, mixin.Scheme), pvccontrol.NewRealPvcControl(mixin.Client, mixin.Scheme), mixin.Recorder),
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

	// Only for starting SharedStrategyController
	err = c.Watch(strategy.SharedStrategyController, &handler.Funcs{})
	if err != nil {
		return err
	}

	ch := make(chan event.GenericEvent, 1<<10)
	strategy.SharedStrategyController.RegisterGenericEventChannel(ch)
	// Watch PodDecoration related events
	err = c.Watch(&source.Channel{Source: ch}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &appsv1alpha1.CollaSet{},
	}, &PodPredicate{})
	if err != nil {
		return err
	}

	return nil
}

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=resourcecontexts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=resourcecontexts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=resourcecontexts/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *CollaSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("collaset", req.String())
	instance := &appsv1alpha1.CollaSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, instance); err != nil {
		if !errors.IsNotFound(err) {
			logger.Error(err, "failed to find CollaSet")
			return reconcile.Result{}, err
		}

		logger.Info("collaSet is deleted")
		return ctrl.Result{}, collasetutils.ActiveExpectations.Delete(req.Namespace, req.Name)
	}

	// if expectation not satisfied, shortcut this reconciling till informer cache is updated.
	if satisfied, err := collasetutils.ActiveExpectations.IsSatisfied(instance); err != nil {
		return ctrl.Result{}, err
	} else if !satisfied {
		logger.Info("CollaSet is not satisfied to reconcile")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if instance.DeletionTimestamp != nil {
		if err := r.ensureReclaimPvcs(ctx, instance); err != nil {
			// reclaim pvcs before remove finalizers
			return ctrl.Result{}, err
		}
		if controllerutil.ContainsFinalizer(instance, preReclaimFinalizer) {
			// reclaim owner IDs in ResourceContext
			if err := r.reclaimResourceContext(instance); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(instance, preReclaimFinalizer) {
		return ctrl.Result{}, controllerutils.AddFinalizer(context.TODO(), r.Client, instance, preReclaimFinalizer)
	}
	key := commonutils.ObjectKeyString(instance)
	currentRevision, updatedRevision, revisions, collisionCount, _, err := r.revisionManager.ConstructRevisions(instance, false)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("fail to construct revision for CollaSet %s: %s", key, err)
	}

	newStatus := &appsv1alpha1.CollaSetStatus{
		// record collisionCount
		CollisionCount:  collisionCount,
		CurrentRevision: currentRevision.Name,
		UpdatedRevision: updatedRevision.Name,
	}

	getter, err := utilspoddecoration.NewPodDecorationGetter(r.Client, instance.Namespace)
	if err != nil {
		return ctrl.Result{}, err
	}
	resources := &collasetutils.RelatedResources{
		Revisions:       revisions,
		CurrentRevision: currentRevision,
		UpdatedRevision: updatedRevision,
		NewStatus:       newStatus,
		PDGetter:        getter,
	}
	requeueAfter, newStatus, err := r.DoReconcile(ctx, instance, resources)
	// update status anyway
	if err := r.updateStatus(ctx, instance, newStatus); err != nil {
		return requeueResult(requeueAfter), fmt.Errorf("fail to update status of CollaSet %s: %s", req, err)
	}

	return requeueResult(requeueAfter), err
}

func (r *CollaSetReconciler) DoReconcile(
	ctx context.Context,
	instance *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources) (
	*time.Duration, *appsv1alpha1.CollaSetStatus, error) {
	podWrappers, requeueAfter, syncErr := r.doSync(ctx, instance, resources)
	return requeueAfter, calculateStatus(instance, resources, podWrappers, syncErr), syncErr
}

// doSync is responsible for reconcile Pods with CollaSet spec.
// 1. sync Pods to prepare information, especially IDs, for following Scale and Update
// 2. scale Pods to match the Pod number indicated in `spec.replcas`. if an error thrown out or Pods is not matched recently, update will be skipped.
// 3. update Pods, to update each Pod to the updated revision indicated by `spec.template`
func (r *CollaSetReconciler) doSync(
	ctx context.Context,
	instance *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources) (
	[]*collasetutils.PodWrapper, *time.Duration, error) {

	synced, podWrappers, ownedIDs, err := r.syncControl.SyncPods(ctx, instance, resources)
	if err != nil || synced {
		return podWrappers, nil, err
	}

	_, scaleRequeueAfter, scaleErr := r.syncControl.Scale(ctx, instance, resources, podWrappers, ownedIDs)
	_, updateRequeueAfter, updateErr := r.syncControl.Update(ctx, instance, resources, podWrappers, ownedIDs)

	err = controllerutils.AggregateErrors([]error{scaleErr, updateErr})
	if updateRequeueAfter != nil && (scaleRequeueAfter == nil || *updateRequeueAfter < *scaleRequeueAfter) {
		return podWrappers, updateRequeueAfter, err
	}
	return podWrappers, scaleRequeueAfter, err
}

func calculateStatus(
	instance *appsv1alpha1.CollaSet,
	resources *collasetutils.RelatedResources,
	podWrappers []*collasetutils.PodWrapper,
	syncErr error) *appsv1alpha1.CollaSetStatus {
	newStatus := resources.NewStatus
	if syncErr == nil {
		newStatus.ObservedGeneration = instance.Generation
	}

	var scheduledReplicas, readyReplicas, availableReplicas, replicas, updatedReplicas, operatingReplicas,
		updatedReadyReplicas, updatedAvailableReplicas int32

	activePods := synccontrol.FilterOutPlaceHolderPodWrappers(podWrappers)
	for _, podWrapper := range activePods {
		replicas++

		isUpdated := false
		if isUpdated = utils.IsPodUpdatedRevision(podWrapper.Pod, resources.UpdatedRevision.Name); isUpdated {
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

		if controllerutils.IsPodServiceAvailable(podWrapper.Pod) {
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

	if (instance.Spec.Replicas == nil && newStatus.UpdatedReadyReplicas >= 0) ||
		newStatus.UpdatedReadyReplicas >= *instance.Spec.Replicas {
		newStatus.CurrentRevision = resources.UpdatedRevision.Name
	}

	return newStatus
}

func (r *CollaSetReconciler) updateStatus(
	ctx context.Context,
	instance *appsv1alpha1.CollaSet,
	newStatus *appsv1alpha1.CollaSetStatus) error {
	if equality.Semantic.DeepEqual(instance.Status, newStatus) {
		return nil
	}

	instance.Status = *newStatus

	err := r.Client.Status().Update(ctx, instance)
	if err == nil {
		return collasetutils.ActiveExpectations.ExpectUpdate(instance, expectations.CollaSet, instance.Name, instance.ResourceVersion)
	}
	return err
}

func (r *CollaSetReconciler) reclaimResourceContext(cls *appsv1alpha1.CollaSet) error {
	// clean the owner IDs from this CollaSet
	if err := podcontext.UpdateToPodContext(r.Client, cls, nil); err != nil {
		return err
	}

	return controllerutils.RemoveFinalizer(context.TODO(), r.Client, cls, preReclaimFinalizer)
}

func requeueResult(requeueTime *time.Duration) reconcile.Result {
	if requeueTime != nil {
		if *requeueTime == 0 {
			return reconcile.Result{Requeue: true}
		}
		return reconcile.Result{RequeueAfter: *requeueTime}
	}
	return reconcile.Result{}
}

func (r *CollaSetReconciler) ensureReclaimPvcs(ctx context.Context, cls *appsv1alpha1.CollaSet) error {
	var needReclaimPvcs []*corev1.PersistentVolumeClaim
	pvcControl := pvccontrol.NewRealPvcControl(r.Client, r.Scheme)
	pvcs, err := pvcControl.GetFilteredPvcs(ctx, cls)
	if err != nil {
		return err
	}
	// reclaim pvcs according to whenDelete retention policy
	for i := range pvcs {
		owned := pvcs[i].OwnerReferences != nil && len(pvcs[i].OwnerReferences) > 0
		if owned && collasetutils.PvcPolicyWhenDelete(cls) == appsv1alpha1.RetainPersistentVolumeClaimRetentionPolicyType {
			needReclaimPvcs = append(needReclaimPvcs, pvcs[i])
		}
	}
	if len(needReclaimPvcs) > 0 {
		_, err = pvcControl.ReleasePvcsOwnerRef(cls, needReclaimPvcs)
	}
	return err
}
