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

package restart

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

func RegisterRestartHandler(methodName string, handler RestartHandler) {
	if registry == nil {
		registry = make(map[string]RestartHandler)
	}
	registry[methodName] = handler
}

func GetRestartHandler(methodName string) RestartHandler {
	if _, exist := registry[methodName]; exist {
		return registry[methodName]
	}
	return nil
}

func GetRestartHandlerFromPod(pod *corev1.Pod) (RestartHandler, string) {
	defaultRestartHandler := GetRestartHandler(KruiseCcontainerRecreateRequest)
	if pod == nil || pod.ObjectMeta.Annotations == nil {
		return nil, "Pod or pod's objectMeta is nil"
	}
	enableKruiseToRestart := feature.DefaultFeatureGate.Enabled(features.EnableKruiseToRestart)

	restartMethodAnno, exist := pod.ObjectMeta.Annotations[appsv1alpha1.AnnotationOperationJobRestartMethod]
	if exist {
		currRestartHandler := GetRestartHandler(restartMethodAnno)
		if currRestartHandler == nil {
			return nil, fmt.Sprintf("Failed to get restart handler: %s", restartMethodAnno)
		}
		if !enableKruiseToRestart && restartMethodAnno == KruiseCcontainerRecreateRequest {
			return nil, "Forbidden to use Kruise crr handler when EnableKruiseToRestart=false"
		}
		return currRestartHandler, ""
	} else {
		if enableKruiseToRestart {
			return defaultRestartHandler, ""
		}
		return nil, "Annotation operationjob.kusionstack.io/restart-method is not set on Pod, or EnableKruiseToRestart=false"
	}
}
