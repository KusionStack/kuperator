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

package podtransitionrule

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"

	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/processor"
	"kusionstack.io/kuperator/pkg/controllers/podtransitionrule/register"
	podtransitionruleutils "kusionstack.io/kuperator/pkg/controllers/podtransitionrule/utils"
	controllerutils "kusionstack.io/kuperator/pkg/controllers/utils"
	"kusionstack.io/kuperator/pkg/utils"
	commonutils "kusionstack.io/kuperator/pkg/utils"
	"kusionstack.io/kuperator/pkg/utils/mixin"
)

const (
	controllerName = "podtransitionrule-controller"
	resourceName   = "PodTransitionRule"
)

// NewReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	mixin := mixin.NewReconcilerMixin(controllerName, mgr)
	return &PodTransitionRuleReconciler{
		ReconcilerMixin: mixin,
		Policy:          register.DefaultPolicy(),
	}
}

func addToMgr(mgr manager.Manager, r reconcile.Reconciler) (controller.Controller, error) {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{
		MaxConcurrentReconciles: 5,
		Reconciler:              r,
	})
	if err != nil {
		return nil, err
	}
	// Watch for changes to PodTransitionRule
	err = c.Watch(&source.Kind{Type: &appsv1alpha1.PodTransitionRule{}}, &PodTransitionRuleEventHandler{})
	if err != nil {
		return c, err
	}

	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &EventHandler{})
	if err != nil {
		return c, err
	}

	err = c.Watch(&source.Channel{Source: NewWebhookGenericEventChannel()}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return c, err
	}
	return c, nil
}

// PodTransitionRuleReconciler reconciles a PodTransitionRule object
type PodTransitionRuleReconciler struct {
	*mixin.ReconcilerMixin
	register.Policy
}

// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=podtransitionrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=podtransitionrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kusionstack.io,resources=podtransitionrules/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

func (r *PodTransitionRuleReconciler) Reconcile(ctx context.Context, request reconcile.Request) (result reconcile.Result, reconcileErr error) {
	logger := r.Logger.WithValues("podTransitionRule", request.String())
	result = reconcile.Result{}
	instance := &appsv1alpha1.PodTransitionRule{}
	if err := r.Client.Get(context.TODO(), request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !podtransitionruleutils.PodTransitionRuleVersionExpectation.SatisfiedExpectations(commonutils.ObjectKeyString(instance), instance.ResourceVersion) {
		logger.Info("podTransitionRule's resourceVersion is too old, retry later", "resourceVersion.now", instance.ResourceVersion)
		return reconcile.Result{}, nil
	}

	selector, _ := metav1.LabelSelectorAsSelector(instance.Spec.Selector)
	selectedPods := &corev1.PodList{}
	if err := r.Client.List(context.TODO(), selectedPods, &client.ListOptions{Namespace: instance.Namespace, LabelSelector: selector}); err != nil {
		logger.Error(err, "failed to list pod by podtransitionrule")
		return reconcile.Result{}, err
	}

	// Delete
	if instance.DeletionTimestamp != nil {
		if err := r.cleanUpPodTransitionRulePods(ctx, instance); err != nil {
			return reconcile.Result{}, err
		}
		if !controllerutil.ContainsFinalizer(instance, appsv1alpha1.ProtectFinalizer) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, controllerutils.RemoveFinalizer(ctx, r.Client, instance, appsv1alpha1.ProtectFinalizer)
	} else if !controllerutil.ContainsFinalizer(instance, appsv1alpha1.ProtectFinalizer) {
		if err := controllerutils.AddFinalizer(ctx, r.Client, instance, appsv1alpha1.ProtectFinalizer); err != nil {
			return result, fmt.Errorf("fail to add finalizer on PodTransitionRule %s: %w", request, err)
		}
	}

	selectedPodNames := sets.String{}
	for _, pod := range selectedPods.Items {
		if !podtransitionruleutils.PodVersionExpectation.SatisfiedExpectations(commonutils.ObjectKeyString(&pod), pod.ResourceVersion) {
			logger.Info("pod's resourceVersion is too old, retry later", "pod", commonutils.ObjectKeyString(&pod), "pod.resourceVersion", pod.ResourceVersion)
			return reconcile.Result{}, nil
		}
		selectedPodNames.Insert(pod.Name)
	}
	targetPods := map[string]*corev1.Pod{}
	for i, pod := range selectedPods.Items {
		targetPods[pod.Name] = &selectedPods.Items[i]
	}

	pods := make([]*corev1.Pod, 0, len(targetPods))
	for _, pod := range targetPods {
		pods = append(pods, pod)
	}

	// sync pod detail by latest instance status
	if err := r.syncPodsDetail(ctx, instance, pods); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to sync pods details: %w", err)
	}

	// remove unselected pods
	for _, name := range instance.Status.Targets {
		if selectedPodNames.Has(name) {
			continue
		}

		if err := r.updatePodTransitionRuleOnPod(ctx, instance.Name, name, instance.Namespace, podtransitionruleutils.MoveAllPodTransitionRuleInfo); err != nil {
			logger.Error(err, "failed to remote podtransitionrule on pod", "pod", name)
			return result, err
		}
	}

	// process rules
	shouldRetry, interval, details, ruleStates := r.process(instance, targetPods)

	res := reconcile.Result{
		Requeue: shouldRetry,
	}
	if interval != nil {
		res.RequeueAfter = *interval
	}

	// TODO: Sync WebhookStates in Details

	detailList := make([]*appsv1alpha1.PodTransitionDetail, 0, len(details))
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	// ensure the order of the slice. (ensure DeepEqual)
	sort.Strings(keys)
	for _, key := range keys {
		detailList = append(detailList, details[key])
	}
	// update podtransitionrule status
	tm := metav1.NewTime(time.Now())
	newStatus := &appsv1alpha1.PodTransitionRuleStatus{
		Targets:            selectedPodNames.List(),
		ObservedGeneration: instance.Generation,
		Details:            detailList,
		RuleStates:         ruleStates,
		UpdateTime:         &tm,
	}

	if !equalStatus(newStatus, &instance.Status) {
		_ = podtransitionruleutils.PodTransitionRuleVersionExpectation.ExpectUpdate(commonutils.ObjectKeyString(instance), instance.ResourceVersion)
		instance.Status = *newStatus
		if err := r.Client.Status().Update(ctx, instance); err != nil {
			podtransitionruleutils.PodTransitionRuleVersionExpectation.DeleteExpectations(commonutils.ObjectKeyString(instance))
			logger.Error(err, "failed to update podtransitionrule status")
			return reconcile.Result{}, err
		}
	}
	return res, nil
}

func (r *PodTransitionRuleReconciler) syncPodsDetail(ctx context.Context, instance *appsv1alpha1.PodTransitionRule, pods []*corev1.Pod) error {
	details := map[string]*appsv1alpha1.PodTransitionDetail{}
	for i, detail := range instance.Status.Details {
		details[detail.Name] = instance.Status.Details[i]
	}
	_, err := controllerutils.SlowStartBatch(len(pods), 1, false, func(i int, _ error) error {
		return r.updatePodDetail(ctx, pods[i], instance.Name, details[pods[i].Name])
	})
	return err
}

func (r *PodTransitionRuleReconciler) updatePodDetail(ctx context.Context, pod *corev1.Pod, podTransitionRuleName string, detail *appsv1alpha1.PodTransitionDetail) error {
	detailAnno := appsv1alpha1.AnnotationPodTransitionRuleDetailPrefix + "/" + podTransitionRuleName
	var newDetail string
	if detail != nil {
		newDetail = utils.DumpJSON(&appsv1alpha1.PodTransitionDetail{Stage: detail.Stage, Passed: detail.Passed, RejectInfo: detail.RejectInfo})
	} else {
		newDetail = utils.DumpJSON(&appsv1alpha1.PodTransitionDetail{Stage: "Unknown", Passed: true})
	}
	if pod.Annotations != nil && pod.Annotations[detailAnno] == newDetail {
		return nil
	}
	patch := client.RawPatch(types.MergePatchType, controllerutils.GetLabelAnnoPatchBytes(nil, nil, nil, map[string]string{detailAnno: newDetail}))
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		return r.Client.Patch(ctx, pod, patch)
	})
}

func (r *PodTransitionRuleReconciler) process(
	rs *appsv1alpha1.PodTransitionRule,
	pods map[string]*corev1.Pod,
) (
	shouldRetry bool,
	interval *time.Duration,
	details map[string]*appsv1alpha1.PodTransitionDetail,
	ruleStates []*appsv1alpha1.RuleState,
) {
	stages := r.GetStages()
	wg := sync.WaitGroup{}
	wg.Add(len(stages))
	mu := sync.RWMutex{}
	details = map[string]*appsv1alpha1.PodTransitionDetail{}
	for _, stage := range stages {
		currentStage := stage
		go func() {
			defer wg.Done()
			res := processor.NewRuleProcessor(r.Client, currentStage, rs, r.Logger).Process(pods)
			mu.Lock()
			defer mu.Unlock()
			if res.Interval != nil {
				if interval == nil || *interval > *res.Interval {
					interval = res.Interval
				}
			}
			if res.RuleStates != nil {
				ruleStates = append(ruleStates, res.RuleStates...)
			}
			if res.Retry {
				shouldRetry = true
			}
			updateDetail(details, res, currentStage)
		}()
	}
	wg.Wait()
	return shouldRetry, interval, details, ruleStates
}

func (r *PodTransitionRuleReconciler) cleanUpPodTransitionRulePods(ctx context.Context, podTransitionRule *appsv1alpha1.PodTransitionRule) error {
	for _, name := range podTransitionRule.Status.Targets {
		if err := r.updatePodTransitionRuleOnPod(ctx, podTransitionRule.Name, name, podTransitionRule.Namespace, podtransitionruleutils.MoveAllPodTransitionRuleInfo); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("fail to remove PodTransitionRule %s on pod %s: %w", commonutils.ObjectKeyString(podTransitionRule), name, err)
		}
	}
	return nil
}

func (r *PodTransitionRuleReconciler) updatePodTransitionRuleOnPod(ctx context.Context, podTransitionRule, name, namespace string, fn func(*corev1.Pod, string) bool) error {
	pod := &corev1.Pod{}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, pod); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		if fn(pod, podTransitionRule) {
			return r.Client.Update(ctx, pod)
		}
		return nil
	})
}

func updateDetail(details map[string]*appsv1alpha1.PodTransitionDetail, passRules *processor.ProcessResult, stage string) {
	for po, rules := range passRules.PassRules {
		var rejectInfo *appsv1alpha1.RejectInfo
		if rej, ok := passRules.Rejected[po]; ok {
			rejectInfo = &appsv1alpha1.RejectInfo{
				RuleName: rej.RuleName,
				Reason:   rej.Reason,
			}
		}
		detail, ok := details[po]
		if !ok {
			detail = &appsv1alpha1.PodTransitionDetail{
				Name:  po,
				Stage: stage,
			}
		}
		detail.PassedRules = append(detail.PassedRules, rules.List()...)
		if rejectInfo != nil {
			detail.RejectInfo = append(detail.RejectInfo, *rejectInfo)
		}
		detail.Passed = detail.RejectInfo == nil || len(detail.RejectInfo) == 0
		details[po] = detail
	}
}

func equalStatus(updated *appsv1alpha1.PodTransitionRuleStatus, current *appsv1alpha1.PodTransitionRuleStatus) bool {
	deepEqual := equality.Semantic.DeepEqual(updated.Targets, current.Targets) &&
		equality.Semantic.DeepEqual(updated.Details, current.Details) &&
		equality.Semantic.DeepEqual(updated.RuleStates, current.RuleStates) &&
		updated.ObservedGeneration == current.ObservedGeneration
	if !deepEqual {
		return utils.DumpJSON(updated) == utils.DumpJSON(current)
	}
	return deepEqual
}
