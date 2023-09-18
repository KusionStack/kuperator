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

package processor

import (
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/ruleset/processor/rules"
	"kusionstack.io/operating/pkg/controllers/ruleset/register"
	"kusionstack.io/operating/pkg/controllers/ruleset/utils"
)

func NewRuleProcessor(client client.Client, stage string, ruleSet *appsv1alpha1.RuleSet, log logr.Logger) *Processor {
	processor := &Processor{
		client:  client,
		stage:   stage,
		ruleSet: ruleSet,
		Logger:  log,
	}
	processor.Policy = register.DefaultPolicy()
	return processor
}

type Processor struct {
	ruleSet *appsv1alpha1.RuleSet
	client  client.Client
	stage   string
	register.Policy
	logr.Logger
}

func (p *Processor) Process(targets map[string]*corev1.Pod) *ProcessResult {
	// some pods on check stage

	var effectiveRules utils.Rules
	for i := range p.ruleSet.Spec.Rules {
		if p.ruleSet.Spec.Rules[i].Disabled || needSkip(&p.ruleSet.Spec.Rules[i]) {
			continue
		}
		if p.ruleSet.Spec.Rules[i].Stage == nil && register.GetRuleStage(&p.ruleSet.Spec.Rules[i].RuleSetRuleDefinition) == p.stage {
			effectiveRules = append(effectiveRules, &p.ruleSet.Spec.Rules[i])
		}
		if p.ruleSet.Spec.Rules[i].Stage != nil && *p.ruleSet.Spec.Rules[i].Stage == p.stage {
			effectiveRules = append(effectiveRules, &p.ruleSet.Spec.Rules[i])
		}
	}

	sort.Sort(effectiveRules)

	effectivePods := sets.NewString()
	processingPods := sets.NewString()
	skipPods := sets.NewString()
	for podName, po := range targets {
		if p.InStage(po, p.stage) {
			processingPods.Insert(podName)
			effectivePods.Insert(podName)
		}
	}

	if processingPods.Len() == 0 {
		return &ProcessResult{}
	}

	passInfo := map[string]sets.String{}
	rejected := map[string]RejectInfo{}
	var ruleStates []*appsv1alpha1.RuleState

	minInterval := time.Duration(math.MaxInt32) * time.Second
	retry := false

	for po := range processingPods {
		passInfo[po] = sets.NewString()
	}

	for _, rule := range effectiveRules {
		// get rule processor
		ruler := rules.GetRuler(rule, p.client)
		if ruler == nil {
			continue
		}
		// skip rule by pod anno
		for _, podName := range processingPods.List() {
			if ok, err := utils.HasSkipRule(targets[podName], rule.Name); ok {
				skipPods.Insert(podName)
				processingPods.Delete(podName)
			} else if err != nil {
				p.Error(err, "fail to get skip rule", "Pod", podName)
			}
		}
		// filter pod with conditions
		if len(rule.Conditions) > 0 {
			for _, podName := range processingPods.List() {
				if len(p.MatchConditions(targets[podName], rule.Conditions...)) == 0 {
					skipPods.Insert(podName)
					processingPods.Delete(podName)
				}
			}
		}
		// rule label match
		if rule.Filter != nil && rule.Filter.LabelSelector != nil {
			selector, _ := metav1.LabelSelectorAsSelector(rule.Filter.LabelSelector)
			for _, podName := range processingPods.List() {
				if !selector.Matches(labels.Set(targets[podName].Labels)) {
					skipPods.Insert(podName)
					processingPods.Delete(podName)
				}
			}
		}

		// do rule processor
		result := ruler.Filter(p.ruleSet, targets, processingPods)

		if result.RuleState != nil {
			ruleStates = append(ruleStates, result.RuleState)
		}

		if result.Err != nil {
			retry = true
			p.Error(result.Err, "ruleset process rule error", "Ruleset", p.ruleSet.Name, "rule", rule.Name)
		}

		// update retry interval
		if result.Interval != nil && *result.Interval < minInterval {
			retry = true
			minInterval = *result.Interval
		}

		for passPodName := range result.Passed {
			passInfo[passPodName].Insert(rule.Name)
		}

		for podName, reason := range result.Rejected {
			rejected[podName] = RejectInfo{Reason: reason, RuleName: rule.Name}
		}

		processingPods = result.Passed.Union(skipPods)
		if processingPods.Len() == 0 {
			break
		}
	}

	res := &ProcessResult{
		Rejected:   rejected,
		PassRules:  passInfo,
		Retry:      retry,
		RuleStates: ruleStates,
	}

	if minInterval != time.Duration(math.MaxInt32) {
		res.Interval = &minInterval
	}
	return res
}

type ProcessResult struct {
	Rejected map[string]RejectInfo
	// pod:rules
	PassRules map[string]sets.String
	Retry     bool
	Interval  *time.Duration

	RuleStates []*appsv1alpha1.RuleState
}

type RejectInfo struct {
	RuleName string
	Reason   string
}

const (
	EnvSkipRuleSetRules = "SKIP_RULESET_RULES"
)

var (
	SkipRuleSetRules = sets.NewString()
)

func needSkip(rule *appsv1alpha1.RuleSetRule) bool {
	typRule := reflect.TypeOf(rule.RuleSetRuleDefinition)
	valRule := reflect.ValueOf(rule.RuleSetRuleDefinition)
	fCount := valRule.NumField()
	for i := 0; i < fCount; i++ {
		if valRule.Field(i).IsNil() {
			continue
		}

		name := strings.ToLower(string(typRule.Field(i).Name[0])) + typRule.Field(i).Name[1:]
		if SkipRuleSetRules.Has(name) {
			return true
		}
	}

	return false
}

func init() {
	valStr := os.Getenv(EnvSkipRuleSetRules)
	if len(valStr) == 0 {
		return
	}
	parts := strings.Split(valStr, ",")
	for _, v := range parts {
		v = strings.TrimSpace(v)
		if len(v) > 0 {
			SkipRuleSetRules.Insert(v)
		}
	}
}
