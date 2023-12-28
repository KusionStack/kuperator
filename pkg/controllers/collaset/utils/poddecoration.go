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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration"
	"kusionstack.io/operating/pkg/utils"
)

type PodDecorationGetter interface {
	GetLatestDecorations() []*appsv1alpha1.PodDecoration
	GetCurrentDecorationsOnPod(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error)
	GetDecorationByRevisions(ctx context.Context, revisions ...string) (map[string]*appsv1alpha1.PodDecoration, error)
	GetLatestDecorationsByTargetLabel(ctx context.Context, labels map[string]string) (map[string]*appsv1alpha1.PodDecoration, error)
	GetUpdatedDecorationsByOldPod(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error)
	GetUpdatedDecorationsByOldRevisions(ctx context.Context, labels map[string]string, oldPDRevisions map[string]string) (map[string]*appsv1alpha1.PodDecoration, error)
}

func NewPodDecorationGetter(ctx context.Context, c client.Client, namespace string) (PodDecorationGetter, error) {
	getter := &podDecorationGetter{
		namespace:                namespace,
		Client:                   c,
		latestPodDecorationNames: sets.NewString(),
		revisions:                map[string]*appsv1alpha1.PodDecoration{},
	}
	return getter, getter.getLatest(ctx)
}

type podDecorationGetter struct {
	client.Client
	namespace string

	latestPodDecorations     []*appsv1alpha1.PodDecoration
	latestPodDecorationNames sets.String
	revisions                map[string]*appsv1alpha1.PodDecoration
}

func (p *podDecorationGetter) GetLatestDecorations() []*appsv1alpha1.PodDecoration {
	return p.latestPodDecorations
}

func (p *podDecorationGetter) GetCurrentDecorationsOnPod(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error) {
	infos := utilspoddecoration.GetDecorationRevisionInfo(pod)
	var revisions []string
	for _, info := range infos {
		revisions = append(revisions, info.Revision)
	}
	return p.GetDecorationByRevisions(ctx, revisions...)
}

func (p *podDecorationGetter) GetDecorationByRevisions(ctx context.Context, revisions ...string) (map[string]*appsv1alpha1.PodDecoration, error) {
	res := map[string]*appsv1alpha1.PodDecoration{}
	var err error
	for _, rev := range revisions {
		if pd, ok := p.revisions[rev]; ok {
			res[rev] = pd
			continue
		}
		pd, localErr := p.getByRevision(ctx, rev)
		if localErr != nil {
			err = utils.Join(err, localErr)
			continue
		}
		res[rev] = pd
	}
	return res, err
}

// GetLatestDecorationsByTargetLabel used to get PodDecorations for a given pod's label.
func (p *podDecorationGetter) GetLatestDecorationsByTargetLabel(ctx context.Context, labels map[string]string) (map[string]*appsv1alpha1.PodDecoration, error) {
	updatedRevisions, stableRevisions := utilspoddecoration.GetEffectiveRevisionsFormLatestDecorations(p.latestPodDecorations, labels)
	return p.GetDecorationByRevisions(ctx, append(updatedRevisions.List(), stableRevisions.List()...)...)
}

func (p *podDecorationGetter) GetUpdatedDecorationsByOldPod(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error) {
	infos := utilspoddecoration.GetDecorationRevisionInfo(pod)
	oldRevisions := map[string]string{}
	for _, info := range infos {
		oldRevisions[info.Name] = info.Revision
	}
	return p.GetUpdatedDecorationsByOldRevisions(ctx, pod.Labels, oldRevisions)
}

func (p *podDecorationGetter) GetUpdatedDecorationsByOldRevisions(ctx context.Context, labels map[string]string, oldPDRevisions map[string]string) (map[string]*appsv1alpha1.PodDecoration, error) {
	updatedRevisions, _ := utilspoddecoration.GetEffectiveRevisionsFormLatestDecorations(p.latestPodDecorations, labels)
	// key: Group name, value: PodDecoration name
	effectiveGroup := map[string]string{}
	updatedPDs, err := p.GetDecorationByRevisions(ctx, updatedRevisions.List()...)
	if err != nil {
		return nil, err
	}
	// delete updated PodDecorations in old revisions
	for _, pd := range updatedPDs {
		if pd.Spec.InjectStrategy.Group != "" {
			effectiveGroup[pd.Spec.InjectStrategy.Group] = pd.Name
		}
		delete(oldPDRevisions, pd.Name)
	}

	var oldStableRevisions []string
	for _, revision := range oldPDRevisions {
		oldStableRevisions = append(oldStableRevisions, revision)
	}

	// get old stable PodDecorations
	oldStablePDs, err := p.GetDecorationByRevisions(ctx, oldStableRevisions...)
	if err != nil {
		return nil, err
	}
	// delete updated group in old stable PodDecorations
	var shouldDeleteRevisions []string
	for rev, pd := range oldStablePDs {
		group := pd.Spec.InjectStrategy.Group
		if group != "" {
			if _, ok := effectiveGroup[group]; ok {
				shouldDeleteRevisions = append(shouldDeleteRevisions, rev)
			}
		}
	}
	for _, rev := range shouldDeleteRevisions {
		delete(oldStablePDs, rev)
	}

	for rev, pd := range oldStablePDs {
		if p.latestPodDecorationNames.Has(pd.Name) {
			updatedPDs[rev] = pd
		}
	}
	return updatedPDs, nil
}

func (p *podDecorationGetter) getByRevision(ctx context.Context, rev string) (*appsv1alpha1.PodDecoration, error) {
	if pd, ok := p.revisions[rev]; ok {
		return pd, nil
	}
	revision := &appsv1.ControllerRevision{}
	if err := p.Get(ctx, types.NamespacedName{Namespace: p.namespace, Name: rev}, revision); err != nil {
		return nil, fmt.Errorf("fail to get PodDecoration ControllerRevision %s/%s: %v", p.namespace, rev, err)
	}
	pd, err := utilspoddecoration.GetPodDecorationFromRevision(revision)
	if err != nil {
		return nil, err
	}
	p.revisions[rev] = pd
	return pd, nil
}

func (p *podDecorationGetter) getLatest(ctx context.Context) (err error) {
	pdList := &appsv1alpha1.PodDecorationList{}
	if err = p.List(ctx, pdList, &client.ListOptions{Namespace: p.namespace}); err != nil {
		return err
	}
	for i := range pdList.Items {
		pd := &pdList.Items[i]
		if pd.Status.UpdatedRevision != "" && pd.Status.ObservedGeneration == pd.Generation {
			p.revisions[pd.Status.UpdatedRevision] = pd
		}
		if pd.Status.IsEffective != nil && *pd.Status.IsEffective && pd.DeletionTimestamp == nil {
			p.latestPodDecorations = append(p.latestPodDecorations, pd)
			p.latestPodDecorationNames.Insert(pd.Name)
		}
	}
	return
}
