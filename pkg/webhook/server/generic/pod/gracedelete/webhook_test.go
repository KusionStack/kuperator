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

package gracedelete

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/kubectl/pkg/scheme"
	"kusionstack.io/operating/apis/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestGraceDelete(t *testing.T) {

	inputs := []struct {
		keyWords string // used to check the error message

		podLabels      map[string]string
		expectedLabels map[string]string

		reqName      string
		reqNamespace string
	}{
		{
			reqName:      "notfounttest",
			reqNamespace: "default",
		},
		{
			reqName:      "uncontrolledtest",
			reqNamespace: "default",
		},
		{
			podLabels: map[string]string{
				fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey): "true",
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, OpsLifecycleAdapter.GetID()):     "testvalue",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, OpsLifecycleAdapter.GetID()): "testvalue",
			},
			reqName:      "test",
			reqNamespace: "default",
			keyWords:     "podOpsLifecycle denied, waiting for pod resource processing",
		},
		{
			podLabels: map[string]string{
				fmt.Sprintf(v1alpha1.ControlledByKusionStackLabelKey):                             "true",
				fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, OpsLifecycleAdapter.GetID()): "true",
			},
			reqName:      "test",
			reqNamespace: "default",
		},
	}

	gd := New()
	for _, v := range inputs {
		client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "test",
					Labels:    v.podLabels,
				},
			}).Build()
		req := admission.Request{
			AdmissionRequest: admissionv1.AdmissionRequest{
				Name:      v.reqName,
				Namespace: v.reqNamespace,
				Operation: admissionv1.Update,
			},
		}

		err := gd.Validating(context.Background(), client, req)
		if v.keyWords == "" {
			assert.Nil(t, err)
		} else {
			assert.NotNil(t, err)
			assert.Contains(t, err.Error(), v.keyWords)
		}
		if len(v.expectedLabels) != 0 {
			pod := &corev1.Pod{}
			client.Get(context.Background(), types.NamespacedName{Namespace: req.Namespace, Name: req.Name}, pod)
			for k, _ := range v.expectedLabels {
				_, exist := pod.Labels[k]
				assert.True(t, exist)
			}
		}
	}
}
