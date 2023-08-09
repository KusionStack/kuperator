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

package podcontext

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kafed/apis/apps/v1alpha1"
	"kusionstack.io/kafed/pkg/controllers/collaset/utils"
	"kusionstack.io/kafed/pkg/controllers/utils/expectations"
)

const (
	OwnerContextKey        = "Owner"
	RevisionContextDataKey = "Revision"
)

func AllocateID(c client.Client, instance *appsv1alpha1.CollaSet, defaultRevision string, replicas int) (map[int]*appsv1alpha1.ContextDetail, error) {
	contextName := getContextName(instance)
	podContext := &appsv1alpha1.ResourceContext{}
	if err := c.Get(context.TODO(), types.NamespacedName{Namespace: instance.Namespace, Name: contextName}, podContext); err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("fail to find ResourceContext %s/%s for owner %s: %s", instance.Namespace, contextName, instance.Name, err)
		}

		podContext.Namespace = instance.Namespace
		podContext.Name = contextName
		if err := c.Create(context.TODO(), podContext); err != nil {
			return nil, fmt.Errorf("fail to create ResourceContext %s/%s for owner %s after not found: %s", instance.Namespace, contextName, instance.Name, err)
		}
	}

	// store all the IDs crossing Multiple workload
	existingIDs := map[int]*appsv1alpha1.ContextDetail{}
	// only store the IDs belonging to this owner
	ownedIDs := map[int]*appsv1alpha1.ContextDetail{}
	for i := range podContext.Spec.Contexts {
		detail := &podContext.Spec.Contexts[i]
		if detail.Contains(OwnerContextKey, instance.Name) {
			ownedIDs[detail.ID] = detail
		}

		existingIDs[detail.ID] = detail
	}

	// if owner has enough ID, return
	if len(ownedIDs) >= replicas {
		return ownedIDs, nil
	}

	// find new IDs for owner
	candidateID := 0
	for len(ownedIDs) < replicas {
		// find one new ID
		for {
			if _, exist := existingIDs[candidateID]; exist {
				candidateID++
				continue
			}

			break
		}

		detail := &appsv1alpha1.ContextDetail{
			ID: candidateID,
			Data: map[string]string{
				OwnerContextKey:        instance.Name,
				RevisionContextDataKey: defaultRevision,
			},
		}
		existingIDs[candidateID] = detail
		ownedIDs[candidateID] = detail
	}

	return ownedIDs, doUpdateToPodContext(c, instance, ownedIDs, podContext, instance.Name)
}

func UpdateToPodContext(c client.Client, instance *appsv1alpha1.CollaSet, ownedIDs map[int]*appsv1alpha1.ContextDetail) error {
	contextName := getContextName(instance)
	podContext := &appsv1alpha1.ResourceContext{}
	if err := c.Get(context.TODO(), types.NamespacedName{Namespace: instance.Namespace, Name: contextName}, podContext); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("fail to find ResourceContext %s/%s: %s", instance.Namespace, contextName, err)
		}

		podContext.Namespace = instance.Namespace
		podContext.Name = contextName
		if err := c.Create(context.TODO(), podContext); err != nil {
			return fmt.Errorf("fail to create ResourceContext %s/%s after not found: %s", instance.Namespace, contextName, err)
		}
	}

	return doUpdateToPodContext(c, instance, ownedIDs, podContext, instance.Name)
}

func doUpdateToPodContext(c client.Client, instance *appsv1alpha1.CollaSet, ownedIDs map[int]*appsv1alpha1.ContextDetail, podContext *appsv1alpha1.ResourceContext, owner string) error {
	// store all IDs crossing all workload
	existingIDs := map[int]*appsv1alpha1.ContextDetail{}
	for k, detail := range ownedIDs {
		existingIDs[k] = detail
	}

	for i := range podContext.Spec.Contexts {
		detail := podContext.Spec.Contexts[i]
		if detail.Contains(OwnerContextKey, owner) {
			continue
		}

		existingIDs[detail.ID] = &detail
	}

	podContext.Spec.Contexts = make([]appsv1alpha1.ContextDetail, len(existingIDs))

	idx := 0
	for _, contextDetail := range existingIDs {
		podContext.Spec.Contexts[idx] = *contextDetail
		idx++
	}

	// keep context detail in order by ID
	sort.Sort(ContextDetailsByOrder(podContext.Spec.Contexts))
	err := c.Update(context.TODO(), podContext)
	if err != nil {
		if err := utils.ActiveExpectations.ExpectUpdate(instance, expectations.ResourceContext, podContext.Name, podContext.ResourceVersion); err != nil {
			return err
		}
	}

	return err
}

func getContextName(instance *appsv1alpha1.CollaSet) string {
	if instance.Spec.ScaleStrategy.Context != "" {
		return instance.Spec.ScaleStrategy.Context
	}

	return instance.Name
}

type ContextDetailsByOrder []appsv1alpha1.ContextDetail

func (s ContextDetailsByOrder) Len() int      { return len(s) }
func (s ContextDetailsByOrder) Swap(i, j int) { s[i], s[j] = s[j], s[i] }

func (s ContextDetailsByOrder) Less(i, j int) bool {
	l, r := s[i], s[j]
	return l.ID < r.ID
}
