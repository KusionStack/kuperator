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

	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsalphav1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils"
)

type collaSetHandler struct {
	client.Client
}

// Create implements EventHandler.
func (c *collaSetHandler) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	colla := evt.Object.(*appsalphav1.CollaSet)
	pdList := &appsalphav1.PodDecorationList{}
	if err := c.List(context.TODO(), pdList, client.InNamespace(colla.Namespace)); err != nil {
		klog.Errorf("fail to list PodDecoration in namespace %s: %v", colla.Namespace, err)
		return
	}
	for _, pd := range pdList.Items {
		if utils.Selected(pd.Spec.Selector, colla.Spec.Template.Labels) {
			q.Add(reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pd)})
		}
	}
}

// Update implements EventHandler.
func (c *collaSetHandler) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
}

// Delete implements EventHandler.
func (c *collaSetHandler) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
}

// Generic implements EventHandler.
func (c *collaSetHandler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
}
