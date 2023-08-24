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
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/ruleset"
	controllerutils "kusionstack.io/kafed/pkg/controllers/utils"
	"kusionstack.io/kafed/pkg/controllers/utils/expectations"
	"kusionstack.io/kafed/pkg/utils"
)

const (
	controllerName = "podopslifecycle-controller"
)

var (
	expectation *expectations.ResourceVersionExpectation
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
			return utils.ControlledByPodOpsLifecycle(newPod)
		},
	})
	if err != nil {
		return err
	}
	return nil
}

var _ reconcile.Reconciler = &ReconcilePodOpsLifecycle{}

func NewReconciler(mgr manager.Manager) *ReconcilePodOpsLifecycle {
	expectation = expectations.NewResourceVersionExpectation()

	r := &ReconcilePodOpsLifecycle{
		Client:         mgr.GetClient(),
		ruleSetManager: ruleset.RuleSetManager(),

		recorder:    mgr.GetEventRecorderFor(controllerName),
		expectation: expectation,
	}
	r.initRuleSetManager()

	return r
}

type ReconcilePodOpsLifecycle struct {
	client.Client
	ruleSetManager ruleset.ManagerInterface
	recorder       record.EventRecorder
	expectation    *expectations.ResourceVersionExpectation
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
func (r *ReconcilePodOpsLifecycle) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	key := fmt.Sprintf("%s/%s", request.Namespace, request.Name)
	klog.Infof("Reconcile Pod %s", key)

	pod := &corev1.Pod{}
	err := r.Client.Get(ctx, request.NamespacedName, pod)
	if err != nil {
		klog.Warningf("failed to get pod %s: %s", key, err)
		if errors.IsNotFound(err) {
			r.expectation.DeleteExpectations(key)
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	state, err := r.ruleSetManager.GetState(r.Client, pod)
	if err != nil {
		klog.Errorf("failed to get pod %s state: %s", key, err)
		return reconcile.Result{}, err
	}

	var (
		stage  string
		labels map[string]string
	)
	if state.InStageAndPassed(v1alpha1.PodOpsLifecyclePreCheckStage) {
		stage = v1alpha1.PodOpsLifecyclePreCheckStage
		labels, err = r.preCheckStage(pod)
	} else if state.InStageAndPassed(v1alpha1.PodOpsLifecyclePostCheckStage) {
		stage = v1alpha1.PodOpsLifecyclePostCheckStage
		labels, err = r.postCheckStage(pod)
	}
	klog.Infof("pod %s in stage %q, labels: %v, error: %v", key, stage, labels, err)
	if err != nil {
		return reconcile.Result{}, err
	}

	if len(labels) > 0 {
		expectation.ExpectUpdate(key, pod.ResourceVersion)
		err = r.addLabels(ctx, pod, labels)
		if err != nil {
			expectation.DeleteExpectations(key)
		}
		return reconcile.Result{}, err
	}

	if !r.expectation.SatisfiedExpectations(key, pod.ResourceVersion) {
		klog.Errorf("skip pod %s with no satisfied", key)
		return reconcile.Result{}, nil
	}

	idToLabelsMap, _, err := PodIDAndTypesMap(pod)
	if err != nil {
		return reconcile.Result{}, err
	}

	expected := map[string]bool{
		v1alpha1.PodPrepareLabelPrefix:  false, // set readiness gate to false, traffic off
		v1alpha1.PodCompleteLabelPrefix: true,  // set readiness gate to true, traffic on
	}
	for _, labels := range idToLabelsMap {
		for k, v := range expected {
			if _, ok := labels[k]; !ok {
				continue
			}

			updated, err := r.updateServiceReadiness(ctx, pod, v)
			if err != nil {
				return reconcile.Result{}, err // only need set once
			}
			if updated {
				return reconcile.Result{}, nil
			}
		}
	}

	_, err = r.updateServiceReadiness(ctx, pod, true)
	return reconcile.Result{}, err
}

func (r *ReconcilePodOpsLifecycle) updateServiceReadiness(ctx context.Context, pod *corev1.Pod, isReady bool) (bool, error) {
	needUpdate, _ := r.setServiceReadiness(pod, isReady)
	if !needUpdate {
		return false, nil
	}

	key := controllerKey(pod)
	r.expectation.ExpectUpdate(key, pod.ResourceVersion)

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.Client.Status().Update(ctx, pod)
	}); err != nil {
		klog.Errorf("failed to update pod status %s: %s", key, err)
		r.expectation.DeleteExpectations(key)

		return false, err
	}
	return true, nil
}

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
	if index == -1 { // append readiness gate
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

	// update readiness gate
	pod.Status.Conditions[index].Status = status
	pod.Status.Conditions[index].LastTransitionTime = metav1.Now()
	pod.Status.Conditions[index].Message = "updated by PodOpsLifecycle"

	return true, fmt.Sprintf("update service readiness gate to: %s", string(status))
}

func (r *ReconcilePodOpsLifecycle) preCheckStage(pod *corev1.Pod) (labels map[string]string, err error) {
	idToLabelsMap, _, err := PodIDAndTypesMap(pod)
	if err != nil {
		return nil, err
	}

	labels = map[string]string{}
	currentTime := strconv.FormatInt(time.Now().Unix(), 10)
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

func (r *ReconcilePodOpsLifecycle) postCheckStage(pod *corev1.Pod) (labels map[string]string, err error) {
	idToLabelsMap, _, err := PodIDAndTypesMap(pod)
	if err != nil {
		return nil, err
	}

	labels = map[string]string{}
	currentTime := strconv.FormatInt(time.Now().Unix(), 10)
	for k := range idToLabelsMap {
		key := fmt.Sprintf("%s/%s", v1alpha1.PodPostCheckedLabelPrefix, k)
		if _, ok := pod.GetLabels()[key]; !ok {
			labels[key] = currentTime // post-checked
		}
	}

	return
}

func (r *ReconcilePodOpsLifecycle) addLabels(ctx context.Context, pod *corev1.Pod, labels map[string]string) error {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	for k, v := range labels {
		pod.Labels[k] = v
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		return r.Client.Update(ctx, pod)
	})
}

func (r *ReconcilePodOpsLifecycle) initRuleSetManager() {
	r.ruleSetManager.RegisterStage(v1alpha1.PodOpsLifecyclePreCheckStage, func(po client.Object) bool {
		labels := po.GetLabels()
		return labels != nil && labelHasPrefix(labels, v1alpha1.PodPreCheckLabelPrefix)
	})
	r.ruleSetManager.RegisterStage(v1alpha1.PodOpsLifecyclePostCheckStage, func(po client.Object) bool {
		labels := po.GetLabels()
		return labels != nil && labelHasPrefix(labels, v1alpha1.PodPostCheckLabelPrefix)
	})
	ruleset.AddUnAvailableFunc(func(po *corev1.Pod) (bool, *int64) {
		return !controllerutils.IsServiceAvailable(po), nil
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
