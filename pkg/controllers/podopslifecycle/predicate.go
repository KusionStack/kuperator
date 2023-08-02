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

package podopslifecycle

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type NeedOpsLifecycle func(oldPod, newPod *v1.Pod) bool

type PodPredicate struct {
	NeedOpsLifecycle // check if pod need use lifecycle
}

func (pp *PodPredicate) Create(evt event.CreateEvent) bool {
	if pp.NeedOpsLifecycle == nil {
		return true
	}

	pod := evt.Object.(*corev1.Pod)
	return pp.NeedOpsLifecycle(nil, pod)
}

func (pp *PodPredicate) Delete(evt event.DeleteEvent) bool {
	return false
}

func (pp *PodPredicate) Update(evt event.UpdateEvent) bool {
	if pp.NeedOpsLifecycle == nil {
		return true
	}

	oldPod := evt.ObjectOld.(*corev1.Pod)
	newPod := evt.ObjectNew.(*corev1.Pod)
	if oldPod == nil && newPod == nil {
		return false
	}
	if !pp.NeedOpsLifecycle(oldPod, newPod) {
		return false
	}

	rv := oldPod.ResourceVersion
	defer func() {
		oldPod.ResourceVersion = rv
	}()

	oldPod.ResourceVersion = newPod.ResourceVersion
	return !equality.Semantic.DeepEqual(oldPod.ObjectMeta, newPod.ObjectMeta)
}

func (pp *PodPredicate) Generic(evt event.GenericEvent) bool {
	if pp.NeedOpsLifecycle == nil {
		return true
	}

	pod := evt.Object.(*corev1.Pod)
	return pp.NeedOpsLifecycle(nil, pod)
}
