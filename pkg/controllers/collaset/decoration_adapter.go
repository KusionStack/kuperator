/*
Copyright 2025 The KusionStack Authors.

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

package collaset

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	xsetapi "kusionstack.io/kube-xset/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	utilspoddecoration "kusionstack.io/kuperator/pkg/controllers/utils/poddecoration"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/anno"
	"kusionstack.io/kuperator/pkg/controllers/utils/poddecoration/strategy"
)

var _ xsetapi.DecorationAdapter = &DecorationAdapter{}

type DecorationAdapter struct{}

func (d *DecorationAdapter) WatchDecoration(c controller.Controller) error {
	// Only for starting SharedStrategyController
	err := c.Watch(strategy.SharedStrategyController, &handler.Funcs{})
	if err != nil {
		return err
	}

	ch := make(chan event.GenericEvent, 1<<10)
	strategy.SharedStrategyController.RegisterGenericEventChannel(ch)
	// Watch PodDecoration related events
	err = c.Watch(&source.Channel{Source: ch}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}
	return nil
}

func (d *DecorationAdapter) GetDecorationGroupVersionKind() metav1.GroupVersionKind {
	return metav1.GroupVersionKind{
		Group:   appsv1alpha1.SchemeGroupVersion.Group,
		Version: appsv1alpha1.SchemeGroupVersion.Version,
		Kind:    "PodDecoration",
	}
}

func (d *DecorationAdapter) GetDecorationPatcherByRevisions(ctx context.Context, c client.Client, target client.Object, revision string) (func(client.Object) error, error) {
	var pds map[string]*appsv1alpha1.PodDecoration
	pod := target.(*corev1.Pod)
	infos, marshallErr := anno.UnmarshallFromString(revision)
	if marshallErr != nil {
		return nil, marshallErr
	}
	var revisions []string
	for _, info := range infos {
		revisions = append(revisions, info.Revision)
	}
	getter, err := utilspoddecoration.NewPodDecorationGetter(c, pod.Namespace)
	if err != nil {
		return nil, err
	}
	pds, err = getter.GetByRevisions(ctx, revisions...)
	if err != nil {
		return nil, err
	}
	return func(object client.Object) error {
		p := object.(*corev1.Pod)
		return utilspoddecoration.PatchListOfDecorations(p, pds)
	}, nil
}

func (d *DecorationAdapter) GetTargetCurrentDecorationRevisions(ctx context.Context, c client.Client, target client.Object) (string, error) {
	var pds map[string]*appsv1alpha1.PodDecoration
	pod := target.(*corev1.Pod)
	getter, err := utilspoddecoration.NewPodDecorationGetter(c, pod.Namespace)
	if err != nil {
		return "", err
	}
	pds, err = getter.GetOnPod(ctx, pod)
	return anno.GetDecorationInfoString(pds), err
}

func (d *DecorationAdapter) GetTargetUpdatedDecorationRevisions(ctx context.Context, c client.Client, target client.Object) (string, error) {
	var pds map[string]*appsv1alpha1.PodDecoration
	pod := target.(*corev1.Pod)
	getter, err := utilspoddecoration.NewPodDecorationGetter(c, pod.Namespace)
	if err != nil {
		return "", err
	}
	pds, err = getter.GetEffective(ctx, pod)
	return anno.GetDecorationInfoString(pds), err
}

func (d *DecorationAdapter) IsTargetDecorationChanged(current, updated string) (bool, error) {
	var currentInfos, updatedInfos []*anno.DecorationInfo
	var err error
	if currentInfos, err = anno.UnmarshallFromString(current); err != nil {
		return false, err
	}
	if updatedInfos, err = anno.UnmarshallFromString(updated); err != nil {
		return false, err
	}

	if len(currentInfos) != len(updatedInfos) {
		return true, nil
	} else {
		revisionSets := sets.NewString()
		for i := range currentInfos {
			revisionSets.Insert(currentInfos[i].Revision)
		}
		for i := range updatedInfos {
			if !revisionSets.Has(updatedInfos[i].Revision) {
				return true, nil
			}
		}
	}
	return false, nil
}
