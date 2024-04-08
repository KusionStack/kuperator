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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	truncatedBy = 1 * time.Second
)

func FormatTime(originTime metav1.Time) metav1.Time {
	newTime := metav1.NewTime(originTime.Truncate(truncatedBy))
	return newTime
}

func FormatTimeNow() metav1.Time {
	return metav1.NewTime(time.Now().Truncate(truncatedBy))
}
