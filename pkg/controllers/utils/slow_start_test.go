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

package utils

import (
	"fmt"
	"sync"
	"testing"
)

const expectedErrMsg = "expected error"

func TestSlowStartBatch(t *testing.T) {
	testcases := []struct {
		name       string
		count      int
		errorIndex *int
		shortcut   bool

		expectedCallCount    int
		expectedSuccessCount int
	}{
		{
			name:                 "happy pass",
			count:                10,
			errorIndex:           nil,
			shortcut:             false,
			expectedCallCount:    10,
			expectedSuccessCount: 10,
		},
		{
			name:                 "failed without shortcut",
			count:                10,
			errorIndex:           intPointer(5),
			shortcut:             false,
			expectedCallCount:    10,
			expectedSuccessCount: 9,
		},
		{
			name:                 "failed without shortcut",
			count:                10,
			errorIndex:           intPointer(5),
			shortcut:             true,
			expectedCallCount:    7, // 1 count in batch 1 + 2 counts in batch 2 + 4 counts in batch 3
			expectedSuccessCount: 6,
		},
	}

	for _, testcase := range testcases {
		callCounter := intPointer(0)
		successCount, err := SlowStartBatch(testcase.count, 1, testcase.shortcut, buildFn(callCounter, testcase.errorIndex))
		if testcase.errorIndex == nil {
			if err != nil {
				t.Fatalf("case %s has unexpected err: %s", testcase.name, err)
			}
		} else {
			if err == nil {
				t.Fatalf("case %s has no expected err", testcase.name)
			} else if err.Error() != expectedErrMsg {
				t.Fatalf("case %s has expected err with unexpected message: %s", testcase.name, err)
			}
		}

		if successCount != testcase.expectedSuccessCount {
			t.Fatalf("case %s gets unexpected success count: expected %d, got %d", testcase.name, testcase.expectedSuccessCount, successCount)
		}

		if *callCounter != testcase.expectedCallCount {
			t.Fatalf("case %s gets unexpected call count: expected %d, got %d", testcase.name, testcase.expectedCallCount, *callCounter)
		}
	}
}

func buildFn(callCounter, errIdx *int) func(int, error) error {
	lock := sync.Mutex{}
	return func(i int, err error) error {
		lock.Lock()
		defer func() {
			*callCounter++
			lock.Unlock()
		}()

		if errIdx != nil && *callCounter == *errIdx {
			return fmt.Errorf(expectedErrMsg)
		}

		return nil
	}
}

func intPointer(val int) *int {
	return &val
}
