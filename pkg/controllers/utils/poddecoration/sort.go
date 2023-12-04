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
	"context"
	"sort"

	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils/inject"
)

type PodDecorations []*appsv1alpha1.PodDecoration

func (br PodDecorations) Len() int {
	return len(br)
}

func (br PodDecorations) Less(i, j int) bool {
	if br[i].Spec.InjectStrategy.Group == br[j].Spec.InjectStrategy.Group {
		if *br[i].Spec.InjectStrategy.Weight == *br[j].Spec.InjectStrategy.Weight {
			br[i].CreationTimestamp.After(br[j].CreationTimestamp.Time)
		}
		return *br[i].Spec.InjectStrategy.Weight > *br[j].Spec.InjectStrategy.Weight
	}
	return br[i].Spec.InjectStrategy.Group < br[j].Spec.InjectStrategy.Group
}

func (br PodDecorations) Swap(i, j int) {
	br[i], br[j] = br[j], br[i]
}

func BuildSortedPodDecorationPointList(list *appsv1alpha1.PodDecorationList) []*appsv1alpha1.PodDecoration {
	res := PodDecorations{}
	for i := range list.Items {
		res = append(res, &list.Items[i])
	}
	sort.Sort(res)
	return res
}

func GetHeaviestPDByGroup(ctx context.Context, c client.Client, namespace, group string) (heaviest *appsv1alpha1.PodDecoration, err error) {
	pdList := &appsv1alpha1.PodDecorationList{}
	if err = c.List(ctx, pdList,
		&client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(
				inject.FieldIndexPodDecorationGroup, group),
			Namespace: namespace,
		}); err != nil {
		return
	}
	podDecorations := BuildSortedPodDecorationPointList(pdList)
	if len(podDecorations) > 0 {
		return podDecorations[0], nil
	}
	return
}

func heaviestPD(a, b *appsv1alpha1.PodDecoration) *appsv1alpha1.PodDecoration {
	if lessPD(a, b) {
		return a
	}
	return b
}

func lessPD(a, b *appsv1alpha1.PodDecoration) bool {
	if a.Spec.InjectStrategy.Group == b.Spec.InjectStrategy.Group {
		if *a.Spec.InjectStrategy.Weight == *b.Spec.InjectStrategy.Weight {
			a.CreationTimestamp.After(b.CreationTimestamp.Time)
		}
		return *a.Spec.InjectStrategy.Weight > *b.Spec.InjectStrategy.Weight
	}
	return a.Spec.InjectStrategy.Group < b.Spec.InjectStrategy.Group
}
