/*
Copyright 2016 The Kubernetes Authors.
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

package utils

import "sync"

const (
	SlowStartInitialBatchSize = 1
)

func IntMin(l, r int) int {
	if l < r {
		return l
	}

	return r
}

func Int32Min(l, r int32) int32 {
	if l < r {
		return l
	}

	return r
}

// SlowStartBatch tries to call the provided function a total of 'count' times,
// starting slow to check for errors, then speeding up if calls succeed.
//
// It groups the calls into batches, starting with a group of initialBatchSize.
// Within each batch, it may call the function multiple times concurrently.
//
// If a whole batch succeeds, the next batch may get exponentially larger.
// If there are any failures in a batch, all remaining batches are skipped
// after waiting for the current batch to complete.
//
// It returns the number of successful calls to the function.
func SlowStartBatch(count int, initialBatchSize int, shortCircuit bool, fn func(int, error) error) (int, error) {
	remaining := count
	successes := 0
	index := 0
	var gotErr error
	for batchSize := IntMin(remaining, initialBatchSize); batchSize > 0; batchSize = IntMin(2*batchSize, remaining) {
		errCh := make(chan error, batchSize)
		var wg sync.WaitGroup
		wg.Add(batchSize)
		for i := 0; i < batchSize; i++ {
			go func(index int) {
				defer wg.Done()
				if err := fn(index, gotErr); err != nil {
					errCh <- err
				}
			}(index)
			index++
		}
		wg.Wait()
		curSuccesses := batchSize - len(errCh)
		successes += curSuccesses
		if len(errCh) > 0 {
			gotErr = <-errCh
			if shortCircuit {
				return successes, gotErr
			}
		}
		remaining -= batchSize
	}
	return successes, gotErr
}
