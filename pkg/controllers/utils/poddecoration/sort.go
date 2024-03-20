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
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type PodDecorations []*appsv1alpha1.PodDecoration

func (br PodDecorations) Len() int {
	return len(br)
}

func (br PodDecorations) Less(i, j int) bool {
	return lessPD(br[i], br[j])
}

func (br PodDecorations) Swap(i, j int) {
	br[i], br[j] = br[j], br[i]
}

func lessPD(a, b *appsv1alpha1.PodDecoration) bool {
	if *a.Spec.Weight == *b.Spec.Weight {
		return a.Name < b.Name
	}
	return *a.Spec.Weight > *b.Spec.Weight
}
