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

package recreate

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	"kusionstack.io/operating/pkg/features"
	"kusionstack.io/operating/pkg/utils/feature"
)

type RestartHandler interface {
	DoRestartContainers(context.Context, client.Client, *appsv1alpha1.OperationJob, *opscontrol.OpsCandidate, []string) error
	GetRestartProgress(context.Context, client.Client, *appsv1alpha1.OperationJob, *opscontrol.OpsCandidate) appsv1alpha1.OperationProgress
	ReleasePod(context.Context, client.Client, *appsv1alpha1.OperationJob, *opscontrol.OpsCandidate) error
}

var registry map[string]RestartHandler

func RegisterRecreateHandler(methodName string, handler RestartHandler) {
	if registry == nil {
		registry = make(map[string]RestartHandler)
	}
	registry[methodName] = handler
}

func GetRecreateHandler(methodName string) RestartHandler {
	if _, exist := registry[methodName]; exist {
		return registry[methodName]
	}
	return nil
}

func GetRecreateHandlerFromPod(pod *corev1.Pod) (RestartHandler, error) {
	defaultRecreateHandler := GetRecreateHandler(KruiseCcontainerRecreateRequest)
	if pod == nil || pod.ObjectMeta.Annotations == nil {
		return nil, fmt.Errorf("")
	}
	enableKruiseToRecreate := feature.DefaultFeatureGate.Enabled(features.EnableKruiseToRecreate)

	recreateMethodAnno, exist := pod.ObjectMeta.Annotations[appsv1alpha1.AnnotationOperationJobRecreateMethod]
	if exist {
		currRecreateHandler := GetRecreateHandler(recreateMethodAnno)
		if currRecreateHandler == nil {
			return nil, fmt.Errorf("cannot find operationjob recreate handler: %s", recreateMethodAnno)
		}
		if !enableKruiseToRecreate && recreateMethodAnno == KruiseCcontainerRecreateRequest {
			return nil, fmt.Errorf("cannot use kruise-containerrecreaterequest handler if EnableKruiseToRecreate is disabled")
		}
		return currRecreateHandler, nil
	} else {
		if enableKruiseToRecreate {
			return defaultRecreateHandler, nil
		}
		return nil, fmt.Errorf("please define and register recreate handler")
	}

}
