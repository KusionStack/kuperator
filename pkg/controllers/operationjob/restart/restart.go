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

	"github.com/go-logr/logr"
	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	. "kusionstack.io/operating/pkg/controllers/operationjob/opscore"
	ojutils "kusionstack.io/operating/pkg/controllers/operationjob/utils"
	"kusionstack.io/operating/pkg/utils/inject"
)

var _ ActionHandler = &KruiseRestartHandler{}

type KruiseRestartHandler struct{}

func (p *KruiseRestartHandler) OperateTarget(ctx context.Context, c client.Client, logger logr.Logger, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) error {
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

func (p *KruiseRestartHandler) GetOpsProgress(
	ctx context.Context, c client.Client, logger logr.Logger, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) (
	progress appsv1alpha1.OperationProgress, reason string, message string, err error) {

	progress = candidate.OpsStatus.Progress
	reason = candidate.OpsStatus.Reason
	message = candidate.OpsStatus.Message

	if candidate.Pod == nil {
		progress = appsv1alpha1.OperationProgressFailed
		reason = appsv1alpha1.ReasonPodNotFound
		return
	}

	if containers, notFound := ojutils.ContainersNotFoundInPod(candidate.Pod, candidate.Containers); notFound {
		progress = appsv1alpha1.OperationProgressFailed
		reason = appsv1alpha1.ReasonContainerNotFound
		message = fmt.Sprintf("Container named %v not found", containers)
		return
	}

	// get crr related to this candidate
	crr, err := getCRRByOperationJobAndPod(ctx, c, operationJob, candidate.PodName)
	if errors.IsNotFound(err) {
		err = nil
		return
	} else if err != nil {
		return
	}

	if crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestCompleted || crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestSucceeded {
		progress = appsv1alpha1.OperationProgressSucceeded
	} else if crr.Status.Phase == kruisev1alpha1.ContainerRecreateRequestFailed {
		progress = appsv1alpha1.OperationProgressFailed
	} else {
		progress = appsv1alpha1.OperationProgressProcessing
	}
	return
}

func (p *KruiseRestartHandler) ReleaseTarget(ctx context.Context, c client.Client, logger logr.Logger, candidate *OpsCandidate, operationJob *appsv1alpha1.OperationJob) error {
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
