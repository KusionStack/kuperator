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
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule/processor"
	"kusionstack.io/operating/pkg/controllers/podtransitionrule/register"
	podtransitionruleutils "kusionstack.io/operating/pkg/controllers/podtransitionrule/utils"
	controllerutils "kusionstack.io/operating/pkg/controllers/utils"
	"kusionstack.io/operating/pkg/utils"
	commonutils "kusionstack.io/operating/pkg/utils"
	"kusionstack.io/operating/pkg/utils/mixin"
)

const (
	controllerName   = "podtransitionrule-controller"
	resourceName     = "PodTransitionRule"
	cleanUpFinalizer = "podtransitionrule.kusionstack.io/need-clean-up"
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
	podTransitionRule := &appsv1alpha1.PodTransitionRule{}
	if err := r.Client.Get(context.TODO(), request.NamespacedName, podTransitionRule); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	if !podtransitionruleutils.PodTransitionRuleVersionExpectation.SatisfiedExpectations(commonutils.ObjectKeyString(podTransitionRule), podTransitionRule.ResourceVersion) {
		logger.Info("podTransitionRule's resourceVersion is too old, retry later", "resourceVersion.now", podTransitionRule.ResourceVersion)
		return reconcile.Result{}, nil
	}

	selector, _ := metav1.LabelSelectorAsSelector(podTransitionRule.Spec.Selector)
	selectedPods := &corev1.PodList{}
	if err := r.Client.List(context.TODO(), selectedPods, &client.ListOptions{Namespace: podTransitionRule.Namespace, LabelSelector: selector}); err != nil {
		logger.Error(err, "failed to list pod by podtransitionrule")
		return reconcile.Result{}, err
	}

	// Delete
	if podTransitionRule.DeletionTimestamp != nil {
		if err := r.cleanUpPodTransitionRulePods(ctx, podTransitionRule); err != nil {
			return reconcile.Result{}, err
		}
		if !controllerutil.ContainsFinalizer(podTransitionRule, cleanUpFinalizer) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, controllerutils.RemoveFinalizer(ctx, r.Client, podTransitionRule, cleanUpFinalizer)
	} else if !controllerutil.ContainsFinalizer(podTransitionRule, cleanUpFinalizer) {
		if err := controllerutils.AddFinalizer(ctx, r.Client, podTransitionRule, cleanUpFinalizer); err != nil {
			return result, fmt.Errorf("fail to add finalizer on PodTransitionRule %s: %s", request, err)
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

	// remove unselected pods
	for _, name := range podTransitionRule.Status.Targets {
		if selectedPodNames.Has(name) {
			continue
		}

		if _, err := r.updatePodTransitionRuleOnPod(ctx, podTransitionRule.Name, name, podTransitionRule.Namespace, podtransitionruleutils.MoveAllPodTransitionRuleInfo); err != nil {
			logger.Error(err, "failed to remote podtransitionrule on pod", "pod", name)
			return result, err
		}
	}

	// process rules
	shouldRetry, interval, details, ruleStates := r.process(podTransitionRule, targetPods)

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
		ObservedGeneration: podTransitionRule.Generation,
		Details:            detailList,
		RuleStates:         ruleStates,
		UpdateTime:         &tm,
	}

	if !equalStatus(newStatus, &podTransitionRule.Status) {
		podtransitionruleutils.PodTransitionRuleVersionExpectation.ExpectUpdate(commonutils.ObjectKeyString(podTransitionRule), podTransitionRule.ResourceVersion)
		podTransitionRule.Status = *newStatus
		if err := r.Client.Status().Update(ctx, podTransitionRule); err != nil {
			podtransitionruleutils.PodTransitionRuleVersionExpectation.DeleteExpectations(commonutils.ObjectKeyString(podTransitionRule))
			logger.Error(err, "failed to update podtransitionrule status")
			return reconcile.Result{}, err
		}
	}
	pods := make([]*corev1.Pod, 0, len(targetPods))
	for _, pod := range targetPods {
		pods = append(pods, pod)
	}
	return res, r.syncPodsDetail(ctx, podTransitionRule.Name, pods, details)
}

func (r *PodTransitionRuleReconciler) syncPodsDetail(ctx context.Context, podTransitionRuleName string, pods []*corev1.Pod, details map[string]*appsv1alpha1.PodTransitionDetail) error {
	_, err := controllerutils.SlowStartBatch(len(pods), 1, false, func(i int, _ error) error {
		return r.updatePodDetail(ctx, pods[i], podTransitionRuleName, details[pods[i].Name])
	})
	return err
}

func (r *PodTransitionRuleReconciler) updatePodDetail(ctx context.Context, pod *corev1.Pod, podTransitionRuleName string, detail *appsv1alpha1.PodTransitionDetail) error {
	detailAnno := appsv1alpha1.AnnotationPodTransitionRuleDetailPrefix + "/" + podTransitionRuleName
	var newDetail string
	if detail != nil {
		newDetail = utils.DumpJSON(&appsv1alpha1.PodTransitionDetail{Stage: detail.Stage, Passed: detail.Passed})
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
		if _, err := r.updatePodTransitionRuleOnPod(ctx, podTransitionRule.Name, name, podTransitionRule.Namespace, podtransitionruleutils.MoveAllPodTransitionRuleInfo); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("fail to remove PodTransitionRule %s on pod %s: %v", commonutils.ObjectKeyString(podTransitionRule), name, err)
		}
	}
	return nil
}

func (r *PodTransitionRuleReconciler) updatePodTransitionRuleOnPod(ctx context.Context, podTransitionRule, name, namespace string, fn func(*corev1.Pod, string) bool) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	return pod, retry.RetryOnConflict(retry.DefaultRetry, func() error {
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

func (r *PodTransitionRuleReconciler) hasRunningPod(pods *corev1.PodList) bool {
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp == nil {
			return true
		}
	}
	return false
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
	return equality.Semantic.DeepEqual(updated.Targets, current.Targets) &&
		equality.Semantic.DeepEqual(updated.Details, current.Details) &&
		equality.Semantic.DeepEqual(updated.RuleStates, current.RuleStates) &&
		updated.ObservedGeneration == current.ObservedGeneration
}
