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

package utils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

func ClearProtection(ctx context.Context, client client.Client, instance *appsv1alpha1.PodOperation) error {
	if !controllerutil.ContainsFinalizer(instance, appsv1alpha1.ProtectFinalizer) {
		return nil
	}
	controllerutil.RemoveFinalizer(instance, appsv1alpha1.ProtectFinalizer)
	return client.Update(ctx, instance)
}

func ProtectPodOperation(ctx context.Context, client client.Client, instance *appsv1alpha1.PodOperation) error {
	if controllerutil.ContainsFinalizer(instance, appsv1alpha1.ProtectFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(instance, appsv1alpha1.ProtectFinalizer)
	return client.Update(ctx, instance)
}
