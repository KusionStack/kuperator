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
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
)

func GetEffectiveDecorationsByCollaSet(
	ctx context.Context,
	c client.Client,
	colla *appsv1alpha1.CollaSet,
) (
	podDecorations []*appsv1alpha1.PodDecoration, oldRevisions map[string]*appsv1alpha1.PodDecoration, err error) {

	pdList := &appsv1alpha1.PodDecorationList{}
	if err = c.List(ctx, pdList, &client.ListOptions{Namespace: colla.Namespace}); err != nil {
		return
	}
	for i := range pdList.Items {
		if isAffectedCollaSet(&pdList.Items[i], colla) {
			podDecorations = append(podDecorations, &pdList.Items[i])
		}
	}
	oldRevisions = map[string]*appsv1alpha1.PodDecoration{}
	for _, pd := range podDecorations {
		if pd.Status.CurrentRevision != "" && pd.Status.CurrentRevision != pd.Status.UpdatedRevision {
			revision := &appsv1.ControllerRevision{}
			if err = c.Get(ctx, types.NamespacedName{Namespace: colla.Namespace, Name: pd.Status.CurrentRevision}, revision); err != nil {
				return nil, nil, fmt.Errorf("fail to get PodDecoration ControllerRevision %s/%s: %v", colla.Namespace, pd.Status.CurrentRevision, err)
			}
			oldPD, err := utilspoddecoration.GetPodDecorationFromRevision(revision)
			if err != nil {
				return nil, nil, err
			}
			oldRevisions[pd.Status.CurrentRevision] = oldPD
		}
	}
	return
}

func isAffectedCollaSet(pd *appsv1alpha1.PodDecoration, colla *appsv1alpha1.CollaSet) bool {
	if pd.Status.IsEffective == nil || !*pd.Status.IsEffective {
		return false
	}
	sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.Selector)
	return sel.Matches(labels.Set(colla.Spec.Template.Labels))
}
