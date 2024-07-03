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

package utils

import (
	"fmt"
	"testing"
)

func TestError(t *testing.T) {
	var errs []error
	err1 := fmt.Errorf("error 1")
	err2 := fmt.Errorf("error 2")
	var err3 error

	actual := AggregateErrors(errs)
	if actual != nil {
		t.Fatalf("expect %v equal to nil", actual)
	}

	errs = append(errs, err1)
	actual = AggregateErrors(errs)
	if actual.Error() != err1.Error() {
		t.Fatalf("expect %v equal to %v", actual, err1)
	}

	errs = append(errs, err2)
	actual = AggregateErrors(errs)
	expected := fmt.Errorf("%v; %v", errs[0], errs[1])
	if actual.Error() != expected.Error() {
		t.Fatalf("expect %v equal to %v", actual, expected)
	}

	errs = append(errs, err3)
	actual = AggregateErrors(errs)
	expected = fmt.Errorf("%v; %v", errs[0], errs[1])
	if actual.Error() != expected.Error() {
		t.Fatalf("expect %v equal to %v", actual, expected)
	}
}
