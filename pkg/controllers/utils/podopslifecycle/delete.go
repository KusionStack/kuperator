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

package podopslifecycle

import (
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var WhenBeginDelete UpdateFunc = func(obj client.Object) (bool, error) {
	return AddLabel(obj.(*corev1.Pod), appsv1alpha1.PodPreparingDeleteLabel, strconv.FormatInt(time.Now().UnixNano(), 10)), nil
}

func AddLabel(po *corev1.Pod, k, v string) bool {
	if po.Labels == nil {
		po.Labels = map[string]string{}
	}
	if _, ok := po.Labels[k]; !ok {
		po.Labels[k] = v
		return true
	}
	return false
}
