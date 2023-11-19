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

package synccontrol

import (
	"sort"

	collasetutils "kusionstack.io/operating/pkg/controllers/collaset/utils"
	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

func getPodsToDelete(filteredPods []*collasetutils.PodWrapper, diff int) []*collasetutils.PodWrapper {
	sort.Sort(ActivePodsForDeletion(filteredPods))
	start := len(filteredPods) - diff
	if start < 0 {
		start = 0
	}
	return filteredPods[start:]
}

type ActivePodsForDeletion []*collasetutils.PodWrapper

func (s ActivePodsForDeletion) Len() int      { return len(s) }
func (s ActivePodsForDeletion) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s ActivePodsForDeletion) Less(i, j int) bool {
	l, r := s[i], s[j]

	lDuringScaleIn := podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, l)
	rDuringScaleIn := podopslifecycle.IsDuringOps(collasetutils.ScaleInOpsLifecycleAdapter, r)

	if lDuringScaleIn && !rDuringScaleIn {
		return true
	}

	if !lDuringScaleIn && rDuringScaleIn {
		return false
	}

	return collasetutils.ComparePod(l.Pod, r.Pod)
}
