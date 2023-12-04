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

package poddecoration

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func IsCollaSetSelectedByPD(collaSet *appsv1alpha1.CollaSet, pd *appsv1alpha1.PodDecoration) bool {
	sel := labels.Everything()
	if pd.Spec.Selector != nil {
		sel, _ = metav1.LabelSelectorAsSelector(pd.Spec.Selector)
	}
	return sel.Matches(labels.Set(collaSet.Spec.Template.Labels))
}
