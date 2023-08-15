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

package podopslifecycle

import (
	"context"
	"fmt"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

const (
	mockLabelKey   = "mockLabel"
	mockLabelValue = "mockLabelValue"
	testNamespace  = "default"
	testName       = "pod-1"
)

var (
	allowTypes = false
	scheme     = runtime.NewScheme()
)

func init() {
	corev1.AddToScheme(scheme)
}

func TestLifecycle(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	g := gomega.NewGomegaWithT(t)

	a := &mockAdapter{id: "id-1", operationType: "type-1"}
	b := &mockAdapter{id: "id-2", operationType: "type-1"}

	inputs := []struct {
		hasOperating, hasConflictID bool
		started                     bool
		err                         error
		allow                       bool
	}{
		{
			hasOperating: false,
			started:      false,
		},
		{
			hasOperating: true,
			started:      false,
		},
		{
			hasConflictID: true,
			started:       false,
			err:           fmt.Errorf("operationType %s exists: %v", a.GetType(), sets.NewString(fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, b.GetID()))),
		},
		{
			hasConflictID: true,
			started:       false,
			allow:         true,
			err:           nil,
		},
	}

	for i, input := range inputs {
		allowTypes = input.allow

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   testNamespace,
				Name:        fmt.Sprintf("%s-%d", testName, i),
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
		}
		g.Expect(c.Create(context.TODO(), pod)).Should(gomega.BeNil())

		if input.hasOperating {
			setOperatingID(a, pod)
			setOperationType(a, pod)
			a.WhenBegin(pod)
		}

		if input.hasConflictID {
			setOperatingID(b, pod)
			setOperationType(b, pod)
		}

		started, err := Begin(c, a, pod)
		g.Expect(reflect.DeepEqual(err, input.err)).Should(gomega.BeTrue())
		if err != nil {
			continue
		}
		g.Expect(pod.Labels[mockLabelKey]).Should(gomega.BeEquivalentTo(mockLabelValue))

		setOperate(a, pod)
		started, err = Begin(c, a, pod)
		g.Expect(err).Should(gomega.BeNil())
		g.Expect(started).Should(gomega.BeTrue())
		g.Expect(IsDuringOps(a, pod)).Should(gomega.BeTrue())

		finished, err := Finish(c, a, pod)
		g.Expect(err).Should(gomega.BeNil())
		g.Expect(finished).Should(gomega.BeTrue())
		g.Expect(pod.Labels[mockLabelKey]).Should(gomega.BeEquivalentTo(""))
		g.Expect(IsDuringOps(a, pod)).Should(gomega.BeFalse())
	}
}

type mockAdapter struct {
	id            string
	operationType OperationType
}

func (m *mockAdapter) GetID() string {
	return m.id
}

func (m *mockAdapter) GetType() OperationType {
	return m.operationType
}

func (m *mockAdapter) AllowMultiType() bool {
	return allowTypes
}

func (m *mockAdapter) WhenBegin(pod client.Object) (bool, error) {
	pod.GetLabels()[mockLabelKey] = mockLabelValue
	return true, nil
}

func (m *mockAdapter) WhenFinish(pod client.Object) (bool, error) {
	delete(pod.GetLabels(), mockLabelKey)
	return true, nil
}
