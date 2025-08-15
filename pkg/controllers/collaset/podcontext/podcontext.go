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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	kubeutilsclient "kusionstack.io/kube-utils/client"
	kubeutilsexpectations "kusionstack.io/kube-utils/controller/expectations"
	"sigs.k8s.io/controller-runtime/pkg/client"

	collasetutils "kusionstack.io/kuperator/pkg/controllers/collaset/utils"
)

const (
	OwnerContextKey              = "Owner"
	RevisionContextDataKey       = "Revision"
	PodDecorationRevisionKey     = "PodDecorationRevisions"
	JustCreateContextDataKey     = "PodJustCreate"
	RecreateUpdateContextDataKey = "PodRecreateUpdate"
)

type Interface interface {
	AllocateID(ctx context.Context, instance *appsv1alpha1.CollaSet, defaultRevision string, replicas int) (map[int]*appsv1alpha1.ContextDetail, error)
	UpdateToPodContext(ctx context.Context, instance *appsv1alpha1.CollaSet, ownedIDs map[int]*appsv1alpha1.ContextDetail) error
}

type RealPodContextControl struct {
	client.Client
	cacheExpectations *kubeutilsexpectations.CacheExpectations
}

func NewRealPodContextControl(c client.Client, cacheExpectations *kubeutilsexpectations.CacheExpectations) Interface {
	return &RealPodContextControl{
		Client:            c,
		cacheExpectations: cacheExpectations,
	}
}

func (r *RealPodContextControl) AllocateID(ctx context.Context, instance *appsv1alpha1.CollaSet, defaultRevision string, replicas int) (map[int]*appsv1alpha1.ContextDetail, error) {
	contextName := getContextName(instance)
	podContext := &appsv1alpha1.ResourceContext{}
	notFound := false
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: contextName}, podContext); err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("fail to find ResourceContext %s/%s for owner %s: %w", instance.Namespace, contextName, instance.Name, err)
		}

		notFound = true
		podContext.Namespace = instance.Namespace
		podContext.Name = contextName
	}

	// store all the IDs crossing Multiple workload
	existingIDs := map[int]*appsv1alpha1.ContextDetail{}
	// only store the IDs belonging to this owner
	ownedIDs := map[int]*appsv1alpha1.ContextDetail{}
	for i := range podContext.Spec.Contexts {
		detail := &podContext.Spec.Contexts[i]
		if detail.Contains(OwnerContextKey, instance.Name) {
			ownedIDs[detail.ID] = detail
			existingIDs[detail.ID] = detail
		} else if instance.Spec.ScaleStrategy.Context != "" {
			// add other collaset podContexts only if context pool enabled
			existingIDs[detail.ID] = detail
		}
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
			// TODO choose just create pods' revision according to scaleStrategy
			Data: map[string]string{
				OwnerContextKey:          instance.Name,
				RevisionContextDataKey:   defaultRevision,
				JustCreateContextDataKey: "true",
			},
		}
		existingIDs[candidateID] = detail
		ownedIDs[candidateID] = detail
	}

	if notFound {
		return ownedIDs, r.doCreatePodContext(ctx, instance, ownedIDs)
	}

	return ownedIDs, r.doUpdatePodContext(ctx, instance, ownedIDs, podContext)
}

func (r *RealPodContextControl) UpdateToPodContext(ctx context.Context, instance *appsv1alpha1.CollaSet, ownedIDs map[int]*appsv1alpha1.ContextDetail) error {
	contextName := getContextName(instance)
	podContext := &appsv1alpha1.ResourceContext{}
	if err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: instance.Namespace, Name: contextName}, podContext); err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("fail to find ResourceContext %s/%s: %w", instance.Namespace, contextName, err)
		}

		if len(ownedIDs) == 0 {
			return nil
		}

		if err := r.doCreatePodContext(ctx, instance, ownedIDs); err != nil {
			return fmt.Errorf("fail to create ResourceContext %s/%s after not found: %w", instance.Namespace, contextName, err)
		}
	}

	return r.doUpdatePodContext(ctx, instance, ownedIDs, podContext)
}

func (r *RealPodContextControl) doCreatePodContext(ctx context.Context, instance *appsv1alpha1.CollaSet, ownerIDs map[int]*appsv1alpha1.ContextDetail) error {
	contextName := getContextName(instance)
	podContext := &appsv1alpha1.ResourceContext{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: instance.Namespace,
			Name:      contextName,
		},
		Spec: appsv1alpha1.ResourceContextSpec{
			Contexts: make([]appsv1alpha1.ContextDetail, len(ownerIDs)),
		},
	}

	i := 0
	for _, detail := range ownerIDs {
		podContext.Spec.Contexts[i] = *detail
		i++
	}

	return r.Client.Create(ctx, podContext)
}

func (r *RealPodContextControl) doUpdatePodContext(ctx context.Context, instance client.Object, ownedIDs map[int]*appsv1alpha1.ContextDetail, podContext *appsv1alpha1.ResourceContext) error {
	// store all IDs crossing all workload
	existingIDs := map[int]*appsv1alpha1.ContextDetail{}

	// add other collaset podContexts only if context pool enabled
	cls := instance.(*appsv1alpha1.CollaSet)
	if cls.Spec.ScaleStrategy.Context != "" {
		for i := range podContext.Spec.Contexts {
			detail := podContext.Spec.Contexts[i]
			if detail.Contains(OwnerContextKey, instance.GetName()) {
				continue
			}
			existingIDs[detail.ID] = &detail
		}
	}

	for _, contextDetail := range ownedIDs {
		existingIDs[contextDetail.ID] = contextDetail
	}

	// delete PodContext if it is empty
	if len(existingIDs) == 0 {
		err := r.Client.Delete(context.TODO(), podContext)
		if err != nil {
			return err
		}
		return r.cacheExpectations.ExpectDeletion(kubeutilsclient.ObjectKeyString(instance), collasetutils.ResourceContextGVK, podContext.Namespace, podContext.Name)
	}

	podContext.Spec.Contexts = make([]appsv1alpha1.ContextDetail, len(existingIDs))
	idx := 0
	for _, contextDetail := range existingIDs {
		podContext.Spec.Contexts[idx] = *contextDetail
		idx++
	}

	// keep context detail in order by ID
	sort.Sort(ContextDetailsByOrder(podContext.Spec.Contexts))
	err := r.Client.Update(ctx, podContext)
	if err != nil {
		return err
	}

	return r.cacheExpectations.ExpectUpdation(kubeutilsclient.ObjectKeyString(instance), collasetutils.ResourceContextGVK, podContext.Namespace, podContext.Name, podContext.ResourceVersion)
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
