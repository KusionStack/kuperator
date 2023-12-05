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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func GetPodEffectiveDecorations(pod *corev1.Pod, podDecorations []*appsv1alpha1.PodDecoration, oldRevisions map[string]*appsv1alpha1.PodDecoration) (res map[string]*appsv1alpha1.PodDecoration) {
	type RevisionPD struct {
		Revision string
		PD       *appsv1alpha1.PodDecoration
	}

	// revision : PD
	res = map[string]*appsv1alpha1.PodDecoration{}
	// group : PD
	currentGroupPD := map[string]*RevisionPD{}

	tryReplace := func(pd *appsv1alpha1.PodDecoration, revision string) {
		current, ok := currentGroupPD[pd.Spec.InjectStrategy.Group]
		if !ok {
			currentGroupPD[pd.Spec.InjectStrategy.Group] = &RevisionPD{
				Revision: revision,
				PD:       pd,
			}
			return
		}
		if lessPD(pd, current.PD) {
			currentGroupPD[pd.Spec.InjectStrategy.Group] = &RevisionPD{
				Revision: revision,
				PD:       pd,
			}
		}
	}
	for i, pd := range podDecorations {
		if pd.Spec.Selector != nil {
			sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.Selector)
			if !sel.Matches(labels.Set(pod.Labels)) {
				continue
			}
		}
		// no rolling upgrade, upgrade all
		if pd.Spec.UpdateStrategy.RollingUpdate == nil {
			tryReplace(podDecorations[i], pd.Status.UpdatedRevision)
			continue
		}
		// by selector
		if pd.Spec.UpdateStrategy.RollingUpdate.Selector != nil {
			sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.UpdateStrategy.RollingUpdate.Selector)
			if sel.Matches(labels.Set(pod.Labels)) {
				tryReplace(podDecorations[i], pd.Status.UpdatedRevision)
			} else if pd.Status.CurrentRevision != "" {
				// use CurrentRevision
				oldPD, ok := oldRevisions[pd.Status.CurrentRevision]
				if ok {
					tryReplace(oldPD, pd.Status.CurrentRevision)
				}
			}
			continue
		}
		// TODO: by partition
		//if pd.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		//}
	}
	for _, revisionPD := range currentGroupPD {
		res[revisionPD.Revision] = revisionPD.PD
	}
	return
}
