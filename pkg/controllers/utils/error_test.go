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
	"errors"
	"fmt"
	"testing"
)

func TestError(t *testing.T) {
	var errs []error
	var nilErr error

	errs = append(errs, nilErr)
	errs = append(errs, fmt.Errorf("error 1"))
	errs = append(errs, fmt.Errorf("error 2"))

	expected := fmt.Errorf("%v; %v", errs[1], errs[2])
	actual := AggregateErrors(errs)
	if errors.Is(expected, actual) {
		t.Fatalf("expect %v equal to %v", actual, expected)
	}
}
