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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/utils/poddecoration/strategy"
	"kusionstack.io/operating/pkg/utils"
)

type Getter interface {
	GetOnPod(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error)
	GetEffective(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error)
	GetByRevisions(ctx context.Context, revisions ...string) (map[string]*appsv1alpha1.PodDecoration, error)
}

type namespacedPodDecorationManager struct {
	c          client.Client
	controller strategy.Reader

	namespace                string
	latestPodDecorations     []*appsv1alpha1.PodDecoration
	latestPodDecorationNames sets.String
	revisions                map[string]*appsv1alpha1.PodDecoration
}

func NewPodDecorationGetter(ctx context.Context, c client.Client, namespace string) (Getter, error) {
	// Wait for PodDecoration strategy manager runnable started
	if !strategy.SharedStrategyController.WaitForSync(ctx) {
		return nil, fmt.Errorf("PodDecorationGetter WaitForCacheSync did not successfully complete")
	}
	getter := &namespacedPodDecorationManager{
		c:                        c,
		controller:               strategy.SharedStrategyController,
		namespace:                namespace,
		latestPodDecorationNames: sets.NewString(),
		revisions:                map[string]*appsv1alpha1.PodDecoration{},
	}
	getter.getLatest()
	return getter, nil
}

func (n *namespacedPodDecorationManager) getLatest() {
	n.latestPodDecorations = n.controller.LatestPodDecorations(n.namespace)
	for i := range n.latestPodDecorations {
		pd := n.latestPodDecorations[i]
		n.latestPodDecorationNames.Insert(pd.Name)
		if pd.Status.UpdatedRevision != "" {
			n.revisions[pd.Status.UpdatedRevision] = pd
		}
	}
}

func (n *namespacedPodDecorationManager) GetLatestDecorations() []*appsv1alpha1.PodDecoration {
	return n.latestPodDecorations
}

func (n *namespacedPodDecorationManager) GetOnPod(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error) {
	infos := GetDecorationRevisionInfo(pod)
	var revisions []string
	for _, info := range infos {
		revisions = append(revisions, info.Revision)
	}
	return n.GetByRevisions(ctx, revisions...)
}

func (n *namespacedPodDecorationManager) GetByRevisions(ctx context.Context, revisions ...string) (map[string]*appsv1alpha1.PodDecoration, error) {
	res := map[string]*appsv1alpha1.PodDecoration{}
	var err error
	for _, rev := range revisions {
		if pd, ok := n.revisions[rev]; ok {
			res[rev] = pd
			continue
		}
		pd, localErr := n.getByRevision(ctx, rev)
		if localErr != nil {
			err = utils.Join(err, localErr)
			continue
		}
		res[rev] = pd
	}
	return res, err
}

func (n *namespacedPodDecorationManager) GetEffective(ctx context.Context, pod *corev1.Pod) (map[string]*appsv1alpha1.PodDecoration, error) {
	infos := GetDecorationRevisionInfo(pod)
	oldRevMap := map[string]string{}
	for _, info := range infos {
		oldRevMap[info.Name] = info.Revision
	}

	updatedRevisions, stableRevisions := n.getEffectiveRevisions(pod, oldRevMap)
	return n.GetByRevisions(ctx, append(updatedRevisions.List(), stableRevisions.List()...)...)
}

func (n *namespacedPodDecorationManager) getEffectiveRevisions(pod *corev1.Pod, oldRevMap map[string]string) (updatedRevisions, stableRevisions sets.String) {
	// oldRevMap, PDName: revision
	// updateRevMap, PDName: revision
	// stableRevMap, PDName: revision
	updatedRevisions = sets.NewString()
	stableRevisions = sets.NewString()
	updateRevMap, stableRevMap := n.controller.EffectivePodRevisions(pod)
	for pd, rev := range updateRevMap {
		updatedRevisions.Insert(rev)
		delete(oldRevMap, pd)
	}
	for pd, rev := range oldRevMap {
		if _, ok := stableRevMap[pd]; ok {
			stableRevisions.Insert(rev)
			delete(stableRevMap, pd)
		}
	}
	for _, rev := range stableRevMap {
		stableRevisions.Insert(rev)
	}
	return updatedRevisions, stableRevisions
}

func (n *namespacedPodDecorationManager) getByRevision(ctx context.Context, rev string) (*appsv1alpha1.PodDecoration, error) {
	if pd, ok := n.revisions[rev]; ok {
		return pd, nil
	}
	revision := &appsv1.ControllerRevision{}
	if err := n.c.Get(ctx, types.NamespacedName{Namespace: n.namespace, Name: rev}, revision); err != nil {
		return nil, fmt.Errorf("fail to get PodDecoration ControllerRevision %s/%s: %v", n.namespace, rev, err)
	}
	pd, err := GetPodDecorationFromRevision(revision)

	if err != nil {
		return nil, err
	}
	n.revisions[rev] = pd
	return pd, nil
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
