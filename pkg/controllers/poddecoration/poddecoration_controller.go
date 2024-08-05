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

package poddecoration

import (
	"context"
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/controllers/utils/expectations"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration/anno"
	"kusionstack.io/operating/pkg/controllers/utils/poddecoration/strategy"
	"kusionstack.io/operating/pkg/controllers/utils/revision"
	"kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

const (
	controllerName = "poddecoration-controller"
)

// Add creates a new PodDecoration Controller and adds it to the Manager with default RBAC.
// The Manager will set fields on the Controller and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcilePodDecoration{
		ReconcilerMixin: mixin.NewReconcilerMixin(controllerName, mgr),
		Client:          mgr.GetClient(),
		revisionManager: revision.NewRevisionManager(mgr.GetClient(), mgr.GetScheme(), &revisionOwnerAdapter{}),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("poddecoration-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to PodDecoration
	err = c.Watch(&source.Kind{Type: &appsv1alpha1.PodDecoration{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	managerClient := mgr.GetClient()

	// Watch update of Pods which can be selected by PodDecoration
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, handler.EnqueueRequestsFromMapFunc(func(podObject client.Object) []reconcile.Request {
		pdList := &appsv1alpha1.PodDecorationList{}
		if listErr := managerClient.List(context.TODO(), pdList, client.InNamespace(podObject.GetNamespace())); listErr != nil {
			return nil
		}
		var requests []reconcile.Request
		for _, pd := range pdList.Items {
			selector, _ := metav1.LabelSelectorAsSelector(pd.Spec.Selector)
			if selector.Matches(labels.Set(podObject.GetLabels())) {
				requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: podObject.GetNamespace(), Name: pd.GetName()}})
			}
		}
		return requests
	}))
	return err
}

var _ reconcile.Reconciler = &ReconcilePodDecoration{}
var (
	statusUpToDateExpectation = expectations.NewResourceVersionExpectation()
)

// ReconcilePodDecoration reconciles a PodDecoration object
type ReconcilePodDecoration struct {
	client.Client
	*mixin.ReconcilerMixin
	revisionManager *revision.RevisionManager
}

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=poddecorations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=poddecorations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=poddecorations/finalizers,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=collasets/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=controllerrevisions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// Reconcile reads that state of the cluster for a PodDecoration object and makes changes based on the state read
// and what is in the PodDecoration.Spec
func (r *ReconcilePodDecoration) Reconcile(ctx context.Context, request reconcile.Request) (res reconcile.Result, reconcileErr error) {
	klog.Infof("Reconcile PodDecoration %v", request)
	// Fetch the PodDecoration instance
	instance := &appsv1alpha1.PodDecoration{}
	if err := r.Get(ctx, request.NamespacedName, instance); err != nil {
		// Object not found, return.  Created objects are automatically garbage collected.
		// For additional cleanup logic use finalizers.
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	key := utils.ObjectKeyString(instance)
	if !statusUpToDateExpectation.SatisfiedExpectations(key, instance.ResourceVersion) {
		klog.Infof("PodDecoration %s is not satisfied with updated status, requeue after, %s", key, instance.ResourceVersion)
		return reconcile.Result{Requeue: true}, nil
	}
	if instance.DeletionTimestamp != nil {
		strategy.SharedStrategyController.DeletePodDecoration(instance)
		statusUpToDateExpectation.DeleteExpectations(key)
		if !r.shouldEscape(ctx, instance) {
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}
		return reconcile.Result{}, r.clearProtection(ctx, instance)
	}
	if err := r.protectPD(ctx, instance); err != nil {
		return reconcile.Result{}, err
	}

	_, updatedRevision, _, collisionCount, _, err := r.revisionManager.ConstructRevisions(instance, false)
	if err != nil {
		return reconcile.Result{}, err
	}
	podList := &corev1.PodList{}
	var selectedPods []*corev1.Pod
	var sel labels.Selector
	if instance.Spec.Selector != nil {
		sel, _ = metav1.LabelSelectorAsSelector(instance.Spec.Selector)
	}
	if err = r.List(ctx, podList, &client.ListOptions{
		Namespace:     instance.Namespace,
		LabelSelector: sel,
	}); err != nil {
		return reconcile.Result{}, err
	}
	for i := range podList.Items {
		selectedPods = append(selectedPods, &podList.Items[i])
	}
	affectedPods, affectedCollaSets, err := r.filterOutPodAndCollaSet(selectedPods)
	if err != nil {
		return reconcile.Result{}, err
	}
	newStatus := &appsv1alpha1.PodDecorationStatus{
		ObservedGeneration: instance.Generation,
		CurrentRevision:    instance.Status.CurrentRevision,
		UpdatedRevision:    updatedRevision.Name,
		CollisionCount:     *collisionCount,
	}
	err = r.calculateStatus(instance, newStatus, affectedPods, affectedCollaSets, instance.Spec.DisablePodDetail)
	if err != nil {
		return reconcile.Result{}, err
	}
	err = r.updateStatus(ctx, instance, newStatus)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, strategy.SharedStrategyController.UpdateSelectedPods(ctx, instance, selectedPods)
}

func (r *ReconcilePodDecoration) calculateStatus(
	instance *appsv1alpha1.PodDecoration,
	status *appsv1alpha1.PodDecorationStatus,
	affectedPods map[string][]*corev1.Pod,
	affectedCollaSets sets.String,
	disablePodDetail bool) error {

	status.MatchedPods = 0
	status.UpdatedPods = 0
	status.UpdatedReadyPods = 0
	status.UpdatedAvailablePods = 0
	status.InjectedPods = 0
	var details []appsv1alpha1.PodDecorationWorkloadDetail
	for collaSet := range affectedCollaSets {
		pods := affectedPods[collaSet]
		detail := appsv1alpha1.PodDecorationWorkloadDetail{
			AffectedReplicas: int32(len(pods)),
			CollaSet:         collaSet,
		}
		status.MatchedPods += int32(len(pods))
		for _, pod := range pods {
			currentRevision := utilspoddecoration.CurrentRevision(pod, instance.Name)
			if currentRevision != nil {
				status.InjectedPods++
				if *currentRevision == status.UpdatedRevision {
					status.UpdatedPods++
					if controllerutils.IsPodReady(pod) {
						status.UpdatedReadyPods++
					}
					if controllerutils.IsPodServiceAvailable(pod) {
						status.UpdatedAvailablePods++
					}
				}
			}
			if !disablePodDetail {
				podInfo := appsv1alpha1.PodDecorationPodInfo{
					Name:    pod.Name,
					Escaped: currentRevision == nil,
				}
				if currentRevision != nil {
					podInfo.Revision = *currentRevision
				}
				detail.Pods = append(detail.Pods, podInfo)
			}
		}
		details = append(details, detail)
	}
	if status.CurrentRevision != status.UpdatedRevision &&
		status.UpdatedPods == status.MatchedPods &&
		r.allCollaSetsSatisfyReplicas(affectedCollaSets, instance.Namespace) {
		status.CurrentRevision = status.UpdatedRevision
	}
	status.Details = details
	return nil
}

func (r *ReconcilePodDecoration) allCollaSetsSatisfyReplicas(collaSets sets.String, ns string) bool {
	collaSet := &appsv1alpha1.CollaSet{}
	for name := range collaSets {
		if err := r.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name}, collaSet); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return false
		}
		// Unreliable in rare cases.
		if collaSet.Status.Replicas != *collaSet.Spec.Replicas {
			return false
		}
	}
	return true
}

func (r *ReconcilePodDecoration) shouldEscape(ctx context.Context, instance *appsv1alpha1.PodDecoration) bool {
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(instance.Namespace),
		client.HasLabels{appsv1alpha1.PodDecorationLabelPrefix + instance.Name},
	); err != nil {
		klog.Errorf("failed to list pods: %v", err)
		return false
	}
	return len(podList.Items) == 0
}

func (r *ReconcilePodDecoration) updateStatus(
	ctx context.Context,
	instance *appsv1alpha1.PodDecoration,
	status *appsv1alpha1.PodDecorationStatus) (err error) {
	if equality.Semantic.DeepEqual(instance.Status, *status) {
		return nil
	}
	statusUpToDateExpectation.ExpectUpdate(utils.ObjectKeyString(instance), instance.ResourceVersion)
	defer func() {
		if err != nil {
			statusUpToDateExpectation.DeleteExpectations(utils.ObjectKeyString(instance))
		}
	}()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		instance.Status = *status
		updateErr := r.Status().Update(ctx, instance)
		if updateErr == nil {
			return nil
		}
		if err := r.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: instance.Name}, instance); err != nil {
			return fmt.Errorf("error getting PodDecoration %s: %v", utils.ObjectKeyString(instance), err)
		}
		return updateErr
	})
}

func (r *ReconcilePodDecoration) filterOutPodAndCollaSet(pods []*corev1.Pod) (
	affectedPods map[string][]*corev1.Pod, affectedCollaSets sets.String, err error) {
	affectedPods = map[string][]*corev1.Pod{}
	affectedCollaSets = sets.NewString()
	for i := range pods {
		ownerRef := metav1.GetControllerOf(pods[i])
		if ownerRef != nil && ownerRef.Kind == "CollaSet" {
			affectedPods[ownerRef.Name] = append(affectedPods[ownerRef.Name], pods[i])
			affectedCollaSets.Insert(ownerRef.Name)
		}
	}
	for key, collaSetPods := range affectedPods {
		sort.Slice(collaSetPods, func(i, j int) bool {
			return collaSetPods[i].Name < collaSetPods[j].Name
		})
		affectedPods[key] = collaSetPods
	}
	return
}

func (r *ReconcilePodDecoration) protectPD(ctx context.Context, pd *appsv1alpha1.PodDecoration) error {
	if controllerutil.ContainsFinalizer(pd, appsv1alpha1.ProtectFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(pd, appsv1alpha1.ProtectFinalizer)
	return r.Update(ctx, pd)
}

func (r *ReconcilePodDecoration) clearProtection(ctx context.Context, pd *appsv1alpha1.PodDecoration) error {
	if !controllerutil.ContainsFinalizer(pd, appsv1alpha1.ProtectFinalizer) {
		return nil
	}
	controllerutil.RemoveFinalizer(pd, appsv1alpha1.ProtectFinalizer)
	return r.Update(ctx, pd)
}
