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

package xcollaset

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	xsetapi "kusionstack.io/kube-xset/api"

	"kusionstack.io/kuperator/pkg/controllers/xcollaset/utils"
)

var _ xsetapi.ResourceContextAdapter = &ResourceContextAdapter{}

var defaultResourceContextKeys = map[xsetapi.ResourceContextKeyEnum]string{
	xsetapi.EnumOwnerContextKey:                     utils.OwnerContextKey,
	xsetapi.EnumRevisionContextDataKey:              utils.RevisionContextDataKey,
	xsetapi.EnumTargetDecorationRevisionKey:         utils.PodDecorationRevisionKey,
	xsetapi.EnumJustCreateContextDataKey:            utils.JustCreateContextDataKey,
	xsetapi.EnumRecreateUpdateContextDataKey:        utils.RecreateUpdateContextDataKey,
	xsetapi.EnumScaleInContextDataKey:               utils.ScaleInContextDataKey,
	xsetapi.EnumReplaceNewTargetIDContextDataKey:    utils.ReplaceNewPodIDContextDataKey,
	xsetapi.EnumReplaceOriginTargetIDContextDataKey: utils.ReplaceOriginPodIDContextDataKey,
}

// ResourceContextAdapter is the adapter to xsetapi apps.kusionstack.io.resourcecontexts
type ResourceContextAdapter struct{}

func (*ResourceContextAdapter) ResourceContextMeta() metav1.TypeMeta {
	return metav1.TypeMeta{APIVersion: appsv1alpha1.SchemeGroupVersion.String(), Kind: "ResourceContext"}
}

func (*ResourceContextAdapter) GetResourceContextSpec(object xsetapi.ResourceContextObject) *xsetapi.ResourceContextSpec {
	rc := object.(*appsv1alpha1.ResourceContext)
	var contexts []xsetapi.ContextDetail
	for i := range rc.Spec.Contexts {
		c := rc.Spec.Contexts[i]
		contexts = append(contexts, xsetapi.ContextDetail{
			ID:   c.ID,
			Data: c.Data,
		})
	}
	return &xsetapi.ResourceContextSpec{
		Contexts: contexts,
	}
}

func (*ResourceContextAdapter) SetResourceContextSpec(spec *xsetapi.ResourceContextSpec, object xsetapi.ResourceContextObject) {
	rc := object.(*appsv1alpha1.ResourceContext)
	var contexts []appsv1alpha1.ContextDetail
	for i := range spec.Contexts {
		c := spec.Contexts[i]
		contexts = append(contexts, appsv1alpha1.ContextDetail{
			ID:   c.ID,
			Data: c.Data,
		})
	}
	rc.Spec.Contexts = contexts
}

func (*ResourceContextAdapter) GetContextKeys() map[xsetapi.ResourceContextKeyEnum]string {
	return defaultResourceContextKeys
}

func (*ResourceContextAdapter) NewResourceContext() xsetapi.ResourceContextObject {
	return &appsv1alpha1.ResourceContext{}
}
