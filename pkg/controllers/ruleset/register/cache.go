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

package register

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var defaultCache = newCache()

func newCache() *cache {
	return &cache{
		stageKey:     sets.NewString(),
		conditionKey: sets.NewString(),
		inStageFunc:  NewFuncCache(),
		condition:    NewFuncCache(),
		pre:          NewFuncCache(),
		post:         NewFuncCache(),
	}
}

type cache struct {
	stageKey    sets.String
	inStageFunc *FuncCache
	stages      []string

	conditionKey sets.String
	condition    *FuncCache

	pre  *FuncCache
	post *FuncCache

	mu sync.Mutex
}

func (r *cache) RegisterStage(stage string, inStage func(obj client.Object) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stages = append(r.stages, stage)
	r.stageKey.Insert(stage)
	r.inStageFunc.Add(stage, inStage)
}

func (r *cache) RegisterCondition(opsCondition string, inCondition func(obj client.Object) bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conditionKey.Insert(opsCondition)
	r.condition.Add(opsCondition, inCondition)
}

func (r *cache) Stage(obj client.Object) string {
	return inFunc(obj, r.stageKey, r.inStageFunc)
}

func (r *cache) GetStages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	res := make([]string, 0, len(r.stages))
	res = append(res, r.stages...)
	return res
}

func (r *cache) InStage(obj client.Object, key string) bool {
	return matchFunc(obj, key, r.inStageFunc)
}

func (r *cache) Conditions(obj client.Object) []string {
	return inFuncs(obj, r.conditionKey, r.condition)
}

func (r *cache) MatchConditions(obj client.Object, conditions ...string) []string {
	nowCds := r.Conditions(obj)
	cds := sets.NewString(conditions...)
	var result []string
	for _, c := range nowCds {
		if cds.Has(c) {
			result = append(result, c)
		}
	}
	return result
}

func inFunc(obj client.Object, keys sets.String, cache *FuncCache) string {
	for k := range keys {
		fs := cache.Get(k)
		needCheck := true
		for _, f := range fs {
			if !f(obj) {
				needCheck = false
				break
			}
		}
		if needCheck {
			return k
		}
	}
	return ""
}

func inFuncs(obj client.Object, keys sets.String, cache *FuncCache) []string {
	var res []string
	for k := range keys {
		fs := cache.Get(k)
		needCheck := true
		for _, f := range fs {
			if !f(obj) {
				needCheck = false
			}
		}
		if needCheck {
			res = append(res, k)
		}
	}
	return res
}

func matchFunc(obj client.Object, key string, cache *FuncCache) bool {
	checkFuncs := cache.Get(key)
	return len(checkFuncs) == 1 && checkFuncs[0](obj)
}
