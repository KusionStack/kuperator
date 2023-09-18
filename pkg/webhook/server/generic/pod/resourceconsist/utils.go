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

package resourceconsist

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/operating/apis/apps/v1alpha1"
)

func GenerateLifecycleFinalizerKey(employer client.Object) string {
	return fmt.Sprintf("%s/%s/%s", employer.GetObjectKind().GroupVersionKind().Kind,
		employer.GetNamespace(), employer.GetName())
}

func GenerateLifecycleFinalizer(employerName string) string {
	b := md5.Sum([]byte(employerName))
	return v1alpha1.PodOperationProtectionFinalizerPrefix + "/" + hex.EncodeToString(b[:])[8:24]
}
