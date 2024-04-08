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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFormatTime(t *testing.T) {
	currentTime := metav1.Now()
	calcTime := FormatTime(currentTime)

	if !calcTime.Before(&currentTime) {
		t.Fatalf("calcTime not before currentTime")
	}
	nano := calcTime.Nanosecond()
	if nano > 0 {
		t.Fatalf("nano large than 0")
	}
}

func TestFormatTimeNow(t *testing.T) {
	calcTime := FormatTimeNow()
	nano := calcTime.Nanosecond()
	if nano > 0 {
		t.Fatalf("nano large than 0")
	}
}
