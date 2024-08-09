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

package strategy

import (
	"context"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	utilspoddecoration "kusionstack.io/operating/pkg/controllers/utils/poddecoration/anno"
)

func getContextName(instance *appsv1alpha1.CollaSet) string {
	if instance.Spec.ScaleStrategy.Context != "" {
		return instance.Spec.ScaleStrategy.Context
	}

	return instance.Name
}

func GetResourceContext(ctx context.Context, c client.Client, instance *appsv1alpha1.CollaSet) (*appsv1alpha1.ResourceContext, error) {
	contextName := getContextName(instance)
	resourceCtx := &appsv1alpha1.ResourceContext{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: instance.Namespace, Name: contextName}, resourceCtx); err != nil {
		return nil, err
	}
	return resourceCtx, nil
}

func IsActive(po *corev1.Pod) bool {
	ownerRef := metav1.GetControllerOf(po)
	return ownerRef != nil &&
		ownerRef.Kind == "CollaSet" &&
		po.DeletionTimestamp == nil
}

func getAllocatedId(context *appsv1alpha1.ResourceContext) sets.String {
	res := sets.NewString()
	for _, detail := range context.Spec.Contexts {
		res.Insert(strconv.Itoa(detail.ID))
	}
	return res
}

func getUpdatedPodDecorationRevision(context *appsv1alpha1.ResourceContext, pdName string) map[string]string {
	res := map[string]string{}
	for _, detail := range context.Spec.Contexts {
		if val, ok := detail.Get("PodDecorationRevisions"); ok {
			pdInfos, err := utilspoddecoration.UnmarshallFromString(val)
			if err == nil {

				for _, pdInfo := range pdInfos {
					if pdInfo.Name == pdName {
						res[strconv.Itoa(detail.ID)] = pdInfo.Revision
						break
					}
				}
			}
		}
	}
	return res
}
