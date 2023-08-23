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

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewFuncCache() *FuncCache {
	return &FuncCache{fc: map[string][]func(client.Object) bool{}}
}

type FuncCache struct {
	fc map[string][]func(obj client.Object) bool
	sync.RWMutex
}

func (n *FuncCache) Get(key string) []func(obj client.Object) bool {
	n.RLock()
	defer n.RUnlock()
	return n.fc[key]
}

func (n *FuncCache) Put(key string, f ...func(obj client.Object) bool) {
	n.Lock()
	defer n.Unlock()
	n.fc[key] = f
}

func (n *FuncCache) Add(key string, f ...func(obj client.Object) bool) {
	n.Lock()
	defer n.Unlock()
	n.fc[key] = append(n.fc[key], f...)
}
