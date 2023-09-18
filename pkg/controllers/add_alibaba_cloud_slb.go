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

package controllers

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"kusionstack.io/operating/pkg/controllers/alibabacloudslb"
	"kusionstack.io/operating/pkg/controllers/resourceconsist"
	"kusionstack.io/operating/pkg/features"
	"kusionstack.io/operating/pkg/utils/feature"
)

func init() {
	AddToManagerFuncs = append(AddToManagerFuncs, Add)
}

func Add(manager manager.Manager) error {
	if !feature.DefaultFeatureGate.Enabled(features.AlibabaCloudSlb) {
		return nil
	}
	reconcileAdapter, err := alibabacloudslb.NewReconcileAdapter(manager.GetClient())
	if err != nil {
		return err
	}
	return resourceconsist.Add(manager, reconcileAdapter)
}
