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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func GetEffectiveRevisionsFormLatestDecorations(latestPodDecorations []*appsv1alpha1.PodDecoration, lb map[string]string) (updatedRevisions, stableRevisions sets.String) {
	groupedDecorations := map[string]*appsv1alpha1.PodDecoration{}
	groupIsUpdatedRevision := map[string]bool{}
	groupRevision := map[string]string{}
	updatedRevisions = sets.NewString()
	stableRevisions = sets.NewString()
	for i, pd := range latestPodDecorations {
		revision, isUpdatedRevision := getEffectiveRevision(pd, lb)
		if revision == "" {
			continue
		}
		// no group PD is effective default
		if pd.Spec.InjectStrategy.Group == "" {
			if isUpdatedRevision {
				updatedRevisions.Insert(revision)
			} else {
				stableRevisions.Insert(revision)
			}
			continue
		}

		// update by heaviest one
		stable, ok := groupedDecorations[pd.Spec.InjectStrategy.Group]
		if !ok || isHeaviest(latestPodDecorations[i], stable) {
			groupedDecorations[pd.Spec.InjectStrategy.Group] = latestPodDecorations[i]
			groupRevision[pd.Spec.InjectStrategy.Group] = revision
			groupIsUpdatedRevision[pd.Spec.InjectStrategy.Group] = isUpdatedRevision
		}
	}
	for group, revision := range groupRevision {
		if groupIsUpdatedRevision[group] {
			updatedRevisions.Insert(revision)
		} else {
			stableRevisions.Insert(revision)
		}
	}
	return
}

func getEffectiveRevision(pd *appsv1alpha1.PodDecoration, lb map[string]string) (string, bool) {
	sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.Selector)
	if !sel.Matches(labels.Set(lb)) && pd.Spec.Selector != nil {
		return "", false
	}
	if inUpdateStrategy(pd, lb) {
		return pd.Status.UpdatedRevision, true
	}
	return pd.Status.CurrentRevision, false
}

// if current is heaviest, return true
func isHeaviest(current, t *appsv1alpha1.PodDecoration) bool {
	if *current.Spec.InjectStrategy.Weight == *t.Spec.InjectStrategy.Weight {
		return current.CreationTimestamp.Time.After(t.CreationTimestamp.Time)
	}
	return *current.Spec.InjectStrategy.Weight > *t.Spec.InjectStrategy.Weight
}

func inUpdateStrategy(pd *appsv1alpha1.PodDecoration, lb map[string]string) bool {
	if pd.Spec.UpdateStrategy.RollingUpdate == nil {
		return true
	}
	if pd.Spec.UpdateStrategy.RollingUpdate.Selector != nil {
		sel, _ := metav1.LabelSelectorAsSelector(pd.Spec.UpdateStrategy.RollingUpdate.Selector)
		if sel.Matches(labels.Set(lb)) {
			return true
		}
	}
	return false
}

func BuildInfo(revisionMap map[string]*appsv1alpha1.PodDecoration) (info string) {
	for k, v := range revisionMap {
		if info == "" {
			info = fmt.Sprintf("{%s: %s}", v.Name, k)
		} else {
			info = info + fmt.Sprintf(", {%s: %s}", v.Name, k)
		}
	}
	return fmt.Sprintf("PodDecorations=[%s]", info)
}
