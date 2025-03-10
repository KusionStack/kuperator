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

package podopslifecycle

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule"
	controllersutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/controllers/utils/expectations"
	"kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

const (
	controllerName = "podopslifecycle-controller"
)

var (
	IsPodReadyFunc = controllersutils.IsPodReady
)

func Add(mgr manager.Manager) error {
	return AddToMgr(mgr, NewReconciler(mgr))
}

func AddToMgr(mgr manager.Manager, r reconcile.Reconciler) error {
	c, err := controller.New(controllerName, mgr, controller.Options{
		MaxConcurrentReconciles: 5,
		Reconciler:              r,
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}, &PodPredicate{
		NeedOpsLifecycle: func(oldPod, newPod *corev1.Pod) bool {
			return utils.ControlledByKusionStack(newPod)
		},
	})
	if err != nil {
		return err
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcilePodOpsLifecycle{}

func NewReconciler(mgr manager.Manager) *ReconcilePodOpsLifecycle {
	mixin := mixin.NewReconcilerMixin(controllerName, mgr)
	expectation := expectations.NewResourceVersionExpectation()

	r := &ReconcilePodOpsLifecycle{
		ReconcilerMixin: mixin,

		podTransitionRuleManager: podtransitionrule.PodTransitionRuleManager(),
		expectation:              expectation,
	}
	r.initPodTransitionRuleManager()

	return r
}

type ReconcilePodOpsLifecycle struct {
	*mixin.ReconcilerMixin

	podTransitionRuleManager podtransitionrule.ManagerInterface
	expectation              *expectations.ResourceVersionExpectation
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

func (r *ReconcilePodOpsLifecycle) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	key := request.String()
	logger := r.Logger.WithValues("pod", key)
	defer logger.Info("reconcile finished")

	pod := &corev1.Pod{}
	err := r.Client.Get(ctx, request.NamespacedName, pod)
	if err != nil {
		logger.Error(err, "failed to get pod")
		if errors.IsNotFound(err) {
			r.expectation.DeleteExpectations(key)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !r.expectation.SatisfiedExpectations(key, pod.ResourceVersion) {
		logger.Info("skip pod with no satisfied")
		return reconcile.Result{}, nil
	}

	if pod.DeletionTimestamp != nil {
		return reconcile.Result{}, nil
	}

	idToLabelsMap, _, err := IDToLabelsMap(pod)
	if err != nil {
		return reconcile.Result{}, err
	}

	// All lifecycles are finished, and should be online
	lifecyclesFinished := len(idToLabelsMap) == 0
	_, stayOffline := pod.Labels[v1alpha1.PodStayOfflineLabel]
	if lifecyclesFinished && !stayOffline {
		updated, err := r.addServiceAvailable(pod)
		if err != nil {
			return reconcile.Result{}, err
		}
		if updated {
			return reconcile.Result{}, nil
		}

		updated, err = r.updateServiceReadiness(ctx, pod, true)
		if err != nil {
			return reconcile.Result{}, err
		}
		if updated {
			return reconcile.Result{}, nil
		}
	}

	// Get the state of pod managed by TransitionRule
	state, err := r.podTransitionRuleManager.GetState(ctx, r.Client, pod)
	if err != nil {
		logger.Error(err, "failed to get pod state")
		return reconcile.Result{}, err
	}

	var labels map[string]string
	if state.InStageAndPassed() {
		switch state.Stage {
		case v1alpha1.PodOpsLifecyclePreCheckStage:
			labels, err = r.preCheckStage(pod, idToLabelsMap)
		case v1alpha1.PodOpsLifecyclePostCheckStage:
			labels, err = r.postCheckStage(pod, idToLabelsMap)
		}
	}
	if err != nil {
		logger.Error(err, "pod in stage information", "stage", state.Stage, "labels", labels)
		return reconcile.Result{}, err
	}
	logger.Info("pod in stage information", "stage", state.Stage, "labels", labels)
	if len(labels) > 0 {
		return reconcile.Result{}, r.addLabels(ctx, pod, labels)
	}

	expected := map[string]bool{
		v1alpha1.PodPreparingLabelPrefix:  false, // Set readiness gate to false
		v1alpha1.PodCompletingLabelPrefix: true,  // Set readiness gate to true
	}
	// preparing phase goes before completing phase
	orderedKeys := []string{v1alpha1.PodPreparingLabelPrefix, v1alpha1.PodCompletingLabelPrefix}
	for _, phaseLabel := range orderedKeys {
		keeped := false
		expectedReadinessGateStatus := expected[phaseLabel]
		for _, labels := range idToLabelsMap {
			if _, ok := labels[phaseLabel]; !ok {
				continue
			}

			updated, err := r.updateServiceReadiness(ctx, pod, expectedReadinessGateStatus)
			if err != nil {
				return reconcile.Result{}, err // Only need set once
			}
			if updated {
				logger.Info("update readiness gate", "status", expectedReadinessGateStatus)
			}
			keeped = true
		}

		// If readiness gate has been set true, it may be set false in other lifecycles, when all lifecycles are finished, it will be set true again.
		if keeped {
			break
		}
	}

	// Remove label service-available if pod is not ready
	if !IsPodReadyFunc(pod) {
		err := r.removeLabels(ctx, pod, []string{v1alpha1.PodServiceAvailableLabel})
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// addServiceAvailable try to add service available label to pod
func (r *ReconcilePodOpsLifecycle) addServiceAvailable(pod *corev1.Pod) (bool, error) {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	if _, ok := pod.Labels[v1alpha1.PodServiceAvailableLabel]; ok {
		return false, nil
	}

	// Whether all expected finalizers are satisfied
	satisfied, notSatisfiedFinalizers, err := controllersutils.IsExpectedFinalizerSatisfied(pod)
	if err != nil {
		return false, err
	}

	if !satisfied {
		allDirty, err := r.removeDirtyExpectedFinalizer(pod, notSatisfiedFinalizers)
		if err != nil {
			return false, err
		}
		if !allDirty {
			return false, nil
		}
		// All not satisfied finalizers are dirty, so actually the pod satisfied expected finalizers now
	}

	if !controllersutils.IsPodReady(pod) {
		return false, nil
	}

	key := controllerKey(pod)
	r.expectation.ExpectUpdate(key, pod.ResourceVersion)
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newPod := &corev1.Pod{}
		err := r.Client.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, newPod)
		if err != nil {
			return err
		}
		if newPod.Labels == nil {
			newPod.Labels = map[string]string{}
		}
		newPod.Labels[v1alpha1.PodServiceAvailableLabel] = strconv.FormatInt(time.Now().UnixNano(), 10)
		delete(newPod.Labels, v1alpha1.PodCreatingLabel)
		delete(newPod.Labels, v1alpha1.PodCompletingLabel)

		return r.Client.Update(context.Background(), newPod)
	})
	if err != nil {
		r.Logger.Error(err, "failed to set Pod as service available", "pod", utils.ObjectKeyString(pod))
		r.expectation.DeleteExpectations(key)
	}

	return true, err
}

func (r *ReconcilePodOpsLifecycle) removeDirtyExpectedFinalizer(pod *corev1.Pod, notSatisfiedFinalizers map[string]string) (bool, error) {
	var allDirty bool // Whether all not atisfied finalizers are dirty
	dirtyExpectedFinalizer := make(map[string]string)

	for expectedFlzKey, finalizer := range notSatisfiedFinalizers {
		isDirty, err := r.isAvailableConditionDirty(pod, expectedFlzKey)
		if err != nil {
			return allDirty, err
		}
		if !isDirty {
			allDirty = false
			break
		}
		dirtyExpectedFinalizer[expectedFlzKey] = finalizer
	}

	if len(dirtyExpectedFinalizer) > 0 {
		podAvailableConditions, err := controllersutils.PodAvailableConditions(pod)
		if err != nil {
			return allDirty, err
		}
		for dirtyExpectedFinalizerKey := range dirtyExpectedFinalizer {
			delete(podAvailableConditions.ExpectedFinalizers, dirtyExpectedFinalizerKey)
		}
		err = r.updateAvailableConditions(pod, podAvailableConditions)
		if err != nil {
			return allDirty, err
		}
	}

	return allDirty, nil
}

func (r *ReconcilePodOpsLifecycle) isAvailableConditionDirty(pod *corev1.Pod, expectedFinalizerKey string) (bool, error) {
	// expectedFinalizerKey is generated under the format(defined in kusionstack.io/resourceconsist):
	// fmt.Sprintf("%s/%s/%s", employer.GetObjectKind().GroupVersionKind().Kind, employer.GetNamespace(), employer.GetName())
	// in kusionstack.io/operating, just check Service since we can't determine how a CR selecting pod
	keySplits := strings.Split(expectedFinalizerKey, "/")
	if len(keySplits) != 3 {
		return false, nil
	}
	if keySplits[0] != "Service" {
		return false, nil
	}

	var svc corev1.Service
	err := r.Client.Get(context.Background(), types.NamespacedName{
		Namespace: keySplits[1],
		Name:      keySplits[2],
	}, &svc)
	if err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}

	if !labels.Set(svc.Spec.Selector).AsSelector().Matches(labels.Set(pod.GetLabels())) {
		return true, nil
	}
	return false, nil
}

func (r *ReconcilePodOpsLifecycle) updateAvailableConditions(pod *corev1.Pod, conditions *v1alpha1.PodAvailableConditions) error {
	newAvailableConditions := utils.DumpJSON(conditions)
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] = newAvailableConditions
	return r.Client.Update(context.Background(), pod)
}

func (r *ReconcilePodOpsLifecycle) updateServiceReadiness(ctx context.Context, pod *corev1.Pod, isReady bool) (bool, error) {
	needUpdate, _ := r.setServiceReadiness(pod, isReady)
	if !needUpdate {
		return false, nil
	}

	key := controllerKey(pod)
	logger := r.Logger.WithValues("pod", key)

	r.expectation.ExpectUpdate(key, pod.ResourceVersion)
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newPod := &corev1.Pod{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, newPod)
		if err != nil {
			return err
		}
		needUpdate, _ := r.setServiceReadiness(newPod, isReady)
		if !needUpdate {
			return nil
		}

		return r.Client.Status().Update(ctx, newPod)
	}); err != nil {
		logger.Error(err, "failed to update pod status")
		r.expectation.DeleteExpectations(key)

		return false, err
	}

	r.Recorder.Eventf(pod, corev1.EventTypeNormal, v1alpha1.ServiceReadyEvent, "Set ReadinessGate service-ready to %v", isReady)

	return true, nil
}

// setServiceReadiness set service readiness gate to pod, and return whether need update pod status
func (r *ReconcilePodOpsLifecycle) setServiceReadiness(pod *corev1.Pod, isReady bool) (bool, string) {
	found := false
	for _, rg := range pod.Spec.ReadinessGates {
		if rg.ConditionType == v1alpha1.ReadinessGatePodServiceReady {
			found = true
		}
	}

	if !found {
		return false, ""
	}

	index := -1
	if pod.Status.Conditions != nil {
		for idx, cond := range pod.Status.Conditions {
			if cond.Type == v1alpha1.ReadinessGatePodServiceReady {
				index = idx
			}
		}
	}

	status := corev1.ConditionTrue
	if !isReady {
		status = corev1.ConditionFalse
	}
	if index == -1 { // Append readiness gate
		pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
			Type:               v1alpha1.ReadinessGatePodServiceReady,
			Status:             status,
			LastTransitionTime: metav1.Now(),
			Message:            "updated by PodOpsLifecycle",
		})
		return true, fmt.Sprintf("append service readiness gate to: %s", string(status))
	}

	if pod.Status.Conditions[index].Status == status {
		return false, ""
	}

	// Update readiness gate
	pod.Status.Conditions[index].Status = status
	pod.Status.Conditions[index].LastTransitionTime = metav1.Now()
	pod.Status.Conditions[index].Message = "updated by PodOpsLifecycle"

	return true, fmt.Sprintf("update service readiness gate to: %s", string(status))
}

func (r *ReconcilePodOpsLifecycle) preCheckStage(pod *corev1.Pod, idToLabelsMap map[string]map[string]string) (labels map[string]string, err error) {
	labels = map[string]string{}
	currentTime := strconv.FormatInt(time.Now().UnixNano(), 10)
	for k, v := range idToLabelsMap {
		t, ok := v[v1alpha1.PodOperationTypeLabelPrefix]
		if !ok {
			continue
		}

		key := fmt.Sprintf("%s/%s", v1alpha1.PodOperationPermissionLabelPrefix, t)
		if _, ok := pod.Labels[key]; !ok {
			labels[key] = currentTime // operation-permission
		}

		key = fmt.Sprintf("%s/%s", v1alpha1.PodPreCheckedLabelPrefix, k)
		if _, ok := pod.Labels[key]; !ok {
			labels[key] = currentTime // pre-checked
		}
	}

	return
}

func (r *ReconcilePodOpsLifecycle) postCheckStage(pod *corev1.Pod, idToLabelsMap map[string]map[string]string) (labels map[string]string, err error) {
	labels = map[string]string{}
	currentTime := strconv.FormatInt(time.Now().UnixNano(), 10)
	for k := range idToLabelsMap {
		key := fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, k)
		if _, ok := pod.Labels[key]; !ok {
			labels[key] = currentTime // post-checked
		}
	}

	return
}

func (r *ReconcilePodOpsLifecycle) addLabels(ctx context.Context, pod *corev1.Pod, labels map[string]string) error {
	if len(labels) == 0 {
		return nil
	}

	key := controllerKey(pod)
	r.expectation.ExpectUpdate(key, pod.ResourceVersion)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newPod := &corev1.Pod{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, newPod)
		if err != nil {
			return err
		}
		if newPod.Labels == nil {
			newPod.Labels = map[string]string{}
		}
		for k, v := range labels {
			newPod.Labels[k] = v
		}
		return r.Client.Update(ctx, newPod)
	})
	if err != nil {
		r.Logger.Error(err, "failed to update pod with labels", "pod", utils.ObjectKeyString(pod), "labels", labels)
		r.expectation.DeleteExpectations(key)
	}

	return err
}

func (r *ReconcilePodOpsLifecycle) removeLabels(ctx context.Context, pod *corev1.Pod, labels []string) error {
	if len(labels) == 0 {
		return nil
	}

	key := controllerKey(pod)
	r.expectation.ExpectUpdate(key, pod.ResourceVersion)
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		newPod := &corev1.Pod{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, newPod)
		if err != nil {
			return err
		}
		for _, label := range labels {
			delete(newPod.Labels, label)
		}
		return r.Client.Update(ctx, newPod)
	})
	if err != nil {
		r.Logger.Error(err, "failed to remove pod labels", "pod", utils.ObjectKeyString(pod), "labels", labels)
		r.expectation.DeleteExpectations(key)
	}

	return err
}

func (r *ReconcilePodOpsLifecycle) initPodTransitionRuleManager() {
	r.podTransitionRuleManager.RegisterStage(v1alpha1.PodOpsLifecyclePreCheckStage, func(po client.Object) bool {
		labels := po.GetLabels()
		return labels != nil && labelHasPrefix(labels, v1alpha1.PodPreCheckLabelPrefix)
	})
	r.podTransitionRuleManager.RegisterStage(v1alpha1.PodOpsLifecyclePostCheckStage, func(po client.Object) bool {
		labels := po.GetLabels()
		return labels != nil && labelHasPrefix(labels, v1alpha1.PodPostCheckLabelPrefix)
	})
	podtransitionrule.AddUnAvailableFunc(func(po *corev1.Pod) (bool, *int64) {
		return !controllersutils.IsPodServiceAvailable(po), nil
	})
}

func controllerKey(pod *corev1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

func labelHasPrefix(labels map[string]string, prefix string) bool {
	for k := range labels {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}
