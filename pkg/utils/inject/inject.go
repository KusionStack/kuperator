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

package inject

import (
	"context"

	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FieldIndexOwnerRefUID            = "ownerRefUID"
	FieldIndexPodTransitionRule      = "podTransitionRuleIndex"
	FieldIndexPodDecorationCollaSets = "podDecorationCollaSets"
)

func NewCacheWithFieldIndex(config *rest.Config, opts cache.Options) (cache.Cache, error) {
	c, err := cache.New(config, opts)
	if err != nil {
		return c, err
	}

	// TODO: opts.SelectorsByObject can be used to limit cache
	//opts.SelectorsByObject = cache.SelectorsByObject{
	//	&corev1.Pod{}: {
	//		Label: labels.Set(map[string]string{v1alpha1.ControlledByKusionStackLabelKey: "true"}).AsSelector(),
	//	},
	//}

	runtime.Must(c.IndexField(
		context.TODO(),
		&appsv1alpha1.PodTransitionRule{},
		FieldIndexPodTransitionRule,
		func(obj client.Object) []string {
			return obj.(*appsv1alpha1.PodTransitionRule).Status.Targets
		}))

	runtime.Must(c.IndexField(
		context.TODO(),
		&appsv1alpha1.PodDecoration{},
		FieldIndexPodDecorationCollaSets,
		func(obj client.Object) (res []string) {
			for _, detail := range obj.(*appsv1alpha1.PodDecoration).Status.Details {
				res = append(res, detail.CollaSet)
			}
			return
		}))

	return c, nil
}
