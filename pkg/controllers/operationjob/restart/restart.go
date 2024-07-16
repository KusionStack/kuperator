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
	"fmt"

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscontrol"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	"kusionstack.io/operating/pkg/utils/inject"
)

type KruiseRestartHandler struct{}

func (p *KruiseRestartHandler) OperateTarget(ctx context.Context, c client.Client, operationJob *appsv1alpha1.OperationJob, candidate *OpsCandidate) error {
	// skip if containers do not exist
	if _, containerNotFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers); containerNotFound {
		return nil
	}

	// create kruise crr to restart container
	_, err := getCRRByOperationJobAndPod(ctx, c, operationJob, candidate.PodName)
	if errors.IsNotFound(err) {
		var crrContainers []kruisev1alpha1.ContainerRecreateRequestContainer
		for _, container := range candidate.Containers {
			crrContainer := kruisev1alpha1.ContainerRecreateRequestContainer{
				Name: container,
			}
			crrContainers = append(crrContainers, crrContainer)
		}

		crr := &kruisev1alpha1.ContainerRecreateRequest{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    operationJob.Namespace,
				Name:         "",
				GenerateName: fmt.Sprintf("%s-", operationJob.Name),
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(operationJob, appsv1alpha1.GroupVersion.WithKind("OperationJob")),
				},
			},
			Spec: kruisev1alpha1.ContainerRecreateRequestSpec{
				PodName:    candidate.PodName,
				Containers: crrContainers,
			},
		}

		if err = c.Create(ctx, crr); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

func (p *KruiseRestartHandler) FulfilTargetOpsStatus(ctx context.Context, c client.Client, operationJob *appsv1alpha1.OperationJob, recorder record.EventRecorder, candidate *OpsCandidate) error {
	if candidate.Pod == nil {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonPodNotFound, "")
		return nil
	}

	if containers, notFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers); notFound {
		MarkCandidateAsFailed(candidate, appsv1alpha1.ReasonContainerNotFound, fmt.Sprintf("Container named %v not found", containers))
		return nil
	}

	crr, err := getCRRByOperationJobAndPod(ctx, c, operationJob, candidate.PodName)
	if errors.IsNotFound(err) {
		candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressPending
		return nil
	} else if err != nil {
		return nil
	}

	if crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestCompleted ||
		crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestSucceeded {
		candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressSucceeded
	} else if crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestFailed {
		candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressFailed
	} else {
		candidate.OpsStatus.Progress = appsv1alpha1.OperationProgressProcessing
	}
	return nil
}

func (p *KruiseRestartHandler) ReleaseTarget(ctx context.Context, c client.Client, operationJob *appsv1alpha1.OperationJob, candidate *OpsCandidate) error {
	if candidate.Pod == nil {
		return nil
	}

	crr, err := getCRRByOperationJobAndPod(ctx, c, operationJob, candidate.PodName)
	if errors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	return c.Delete(ctx, crr)

}

func getCRRByOperationJobAndPod(ctx context.Context, c client.Client, instance *appsv1alpha1.OperationJob, podName string) (*kruisev1alpha1.ContainerRecreateRequest, error) {
	crrList := &kruisev1alpha1.ContainerRecreateRequestList{}
	if err := c.List(ctx, crrList, &client.ListOptions{Namespace: instance.Namespace, FieldSelector: fields.OneTermEqualSelector(inject.FieldIndexOwnerRefUID, string(instance.GetUID()))}); err != nil {
		return nil, err
	}
	for i := range crrList.Items {
		if crrList.Items[i].Spec.PodName == podName {
			return &crrList.Items[i], nil
		}
	}
	return nil, errors.NewNotFound(schema.GroupResource{Resource: "containerrecreaterequest"}, fmt.Sprintf("%s-", instance.Name))
}
