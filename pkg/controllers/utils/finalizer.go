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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func ObjectKey(obj client.Object) string {
	if obj.GetNamespace() == "" {
		return obj.GetName()
	}
	return obj.GetNamespace() + "/" + obj.GetName()
}

func RemoveFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string) error {
	if !controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		controllerutil.RemoveFinalizer(obj, finalizer)
		var updateErr error
		if updateErr = c.Update(ctx, obj); updateErr == nil {
			return nil
		}
		if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, obj); err != nil {
			return err
		}
		return updateErr
	})
}

func AddFinalizer(ctx context.Context, c client.Client, obj client.Object, finalizer string) error {
	if controllerutil.ContainsFinalizer(obj, finalizer) {
		return nil
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		controllerutil.AddFinalizer(obj, finalizer)
		var updateErr error
		if updateErr = c.Update(ctx, obj); updateErr == nil {
			return nil
		}
		if err := c.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, obj); err != nil {
			return err
		}
		return updateErr
	})
}

func ContainsFinalizer(obj client.Object, finalizer string) bool {
	for _, f := range obj.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}

	return false
}
