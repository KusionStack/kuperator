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

package podoperation

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

type PodHandler struct {
	client.Client
}

func (p *PodHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	pod := evt.Object.(*corev1.Pod)
	enqueueForObject(p.Client, pod, &q)
}

func (p *PodHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	pod := evt.Object.(*corev1.Pod)
	enqueueForObject(p.Client, pod, &q)
}

func (p *PodHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	pod := evt.ObjectOld.(*corev1.Pod)
	enqueueForObject(p.Client, pod, &q)
}

func (p *PodHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	pod := evt.Object.(*corev1.Pod)
	enqueueForObject(p.Client, pod, &q)
}

func enqueueForObject(c client.Client, pod *corev1.Pod, q *workqueue.RateLimitingInterface) {
	if pooNames, owned := controlledByPodOperation(c, pod); owned {
		for pooName := range pooNames {
			(*q).Add(reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: pod.Namespace,
					Name:      pooName,
				},
			})
		}
	}
}

func controlledByPodOperation(c client.Client, pod *corev1.Pod) (sets.String, bool) {
	pooNames := sets.String{}
	pooList := &appsv1alpha1.PodOperationList{}
	if listErr := c.List(context.TODO(), pooList, client.InNamespace(pod.Namespace)); listErr != nil {
		return pooNames, false
	}

	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	originPodName := pod.Labels[appsv1alpha1.PodReplacePairOriginName]

	for _, poo := range pooList.Items {
		for _, target := range poo.Spec.Targets {
			if pod.Name == target.PodName {
				pooNames.Insert(poo.Name)
				break
			}
			if originPodName == target.PodName {
				pooNames.Insert(poo.Name)
				break
			}
		}
		for _, status := range poo.Status.PodDetails {
			if status.ExtraInfo != nil && status.ExtraInfo[appsv1alpha1.ReplacePodNameKey] == pod.Name {
				pooNames.Insert(poo.Name)
				break
			}
		}
	}

	return pooNames, pooNames.Len() > 0
}
