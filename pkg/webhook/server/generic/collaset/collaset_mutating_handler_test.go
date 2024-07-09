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

package collaset

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func TestMutatingCollaSet(t *testing.T) {
	cls := &appsv1alpha1.CollaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
		},
		Spec: appsv1alpha1.CollaSetSpec{},
	}

	appsv1alpha1.SetDefaultCollaSet(cls)

	if cls.Spec.UpdateStrategy.PodUpdatePolicy != appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType {
		t.Fatalf("expected default value is %s, got %s", appsv1alpha1.CollaSetInPlaceIfPossiblePodUpdateStrategyType,
			cls.Spec.UpdateStrategy.PodUpdatePolicy)
	}

	if cls.Spec.UpdateStrategy.RollingUpdate == nil {
		t.Fatalf("expected default rollingUpdate, got nil")
	}

	if cls.Spec.UpdateStrategy.RollingUpdate.ByPartition == nil {
		t.Fatalf("expected default byPartition, got nil")
	}
}
