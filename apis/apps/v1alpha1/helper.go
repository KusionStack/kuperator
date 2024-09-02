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

package v1alpha1

import (
	"fmt"
	"hash"
	"hash/fnv"
	"sync"
)

var (
	hashPool = sync.Pool{
		New: func() any {
			return fnv.New32a()
		},
	}
)

func GenerateLifecycleID(v string) string {
	if len(v) > 63 {
		// https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set
		hp := hashPool.Get().(hash.Hash32)
		hp.Write([]byte(v))
		key := fmt.Sprintf("%#x", hp.Sum32())
		hp.Reset()
		hashPool.Put(hp)
		return key
	}
	return v
}
