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

package replace

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
)

type OriginPodHandler struct {
	client.Client
}

func (p *OriginPodHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	pod := evt.Object.(*corev1.Pod)
	ojutils.EnqueueOperationJobFromPod(p.Client, pod, &q, replacedByOperationJob)
}

func (p *OriginPodHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	pod := evt.Object.(*corev1.Pod)
	ojutils.EnqueueOperationJobFromPod(p.Client, pod, &q, replacedByOperationJob)
}

func (p *OriginPodHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	pod := evt.ObjectOld.(*corev1.Pod)
	ojutils.EnqueueOperationJobFromPod(p.Client, pod, &q, replacedByOperationJob)
}

func (p *OriginPodHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	pod := evt.Object.(*corev1.Pod)
	ojutils.EnqueueOperationJobFromPod(p.Client, pod, &q, replacedByOperationJob)
}

func replacedByOperationJob(c client.Client, pod *corev1.Pod) (sets.String, bool) {
	ojNames := sets.String{}
	ojList := &appsv1alpha1.OperationJobList{}
	if listErr := c.List(context.TODO(), ojList, client.InNamespace(pod.Namespace)); listErr != nil {
		return ojNames, false
	}

	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	originPodName := pod.Labels[appsv1alpha1.PodReplacePairOriginName]

	for _, oj := range ojList.Items {
		for _, target := range oj.Spec.Targets {
			if originPodName == target.Name {
				ojNames.Insert(oj.Name)
				break
			}
		}
	}

	return ojNames, ojNames.Len() > 0
}
