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

package poddecoration

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
)

var _ = Describe("PodDecoration controller", func() {
	It("patch metadata, RetainMetadata and OverwriteMetadata", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"retain":    "bar",
					"overwrite": "bar",
				},
				Annotations: map[string]string{
					"retain":    "bar",
					"overwrite": "bar",
				},
			},
		}
		template := &appsv1alpha1.PodDecorationPodTemplate{
			Metadata: []*appsv1alpha1.PodDecorationPodTemplateMeta{
				{
					PatchPolicy: appsv1alpha1.RetainMetadata,
					Labels: map[string]string{
						"retain": "xxx",
						"new":    "xxx",
					},
					Annotations: map[string]string{
						"retain": "xxx",
						"new":    "xxx",
					},
				},
				{
					PatchPolicy: appsv1alpha1.OverwriteMetadata,
					Labels: map[string]string{
						"overwrite": "xxx",
					},
					Annotations: map[string]string{
						"overwrite": "xxx",
					},
				},
			},
		}
		Expect(PatchPodDecoration(pod, template)).Should(BeNil())
		Expect(pod.Labels["retain"]).Should(Equal("bar"))
		Expect(pod.Labels["overwrite"]).Should(Equal("xxx"))
		Expect(pod.Labels["new"]).Should(Equal("xxx"))
		Expect(pod.Annotations["retain"]).Should(Equal("bar"))
		Expect(pod.Annotations["overwrite"]).Should(Equal("xxx"))
		Expect(pod.Annotations["new"]).Should(Equal("xxx"))
	})

	It("patch metadata, MergePatchJsonMetadata", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"foo": "{\"aaa\":\"123\",\"bbb\":\"234\"}",
				},
			},
		}
		template := &appsv1alpha1.PodDecorationPodTemplate{
			Metadata: []*appsv1alpha1.PodDecorationPodTemplateMeta{
				{
					PatchPolicy: appsv1alpha1.MergePatchJsonMetadata,
					Annotations: map[string]string{
						"foo": "{\"ccc\":\"789\"}",
					},
				},
			},
		}
		Expect(PatchPodDecoration(pod, template)).Should(BeNil())
		Expect(pod.Annotations["foo"]).Should(Equal("{\"aaa\":\"123\",\"bbb\":\"234\",\"ccc\":\"789\"}"))
	})

	It("patch InitContainers", func() {
		pod := &v1.Pod{}
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			InitContainers: []*v1.Container{
				{
					Name:  "foo",
					Image: "nginx:v1",
				},
			},
		})).Should(BeNil())
		Expect(len(pod.Spec.InitContainers)).Should(Equal(1))
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			InitContainers: []*v1.Container{
				{
					Name:  "foo",
					Image: "nginx:v2",
				},
			},
		})).Should(BeNil())
		Expect(pod.Spec.InitContainers[0].Image).Should(Equal("nginx:v1"))
	})

	It("patch Containers", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "foo",
						Image: "nginx:v1",
					},
				},
			},
		}
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			Containers: []*appsv1alpha1.ContainerPatch{
				{
					InjectPolicy: appsv1alpha1.BeforePrimaryContainer,
					Container: v1.Container{
						Name:  "foo-sidecar",
						Image: "nginx:v1",
					},
				},
			},
		})).Should(BeNil())
		Expect(len(pod.Spec.Containers)).Should(Equal(2))
	})

	It("patch PrimaryContainers", func() {
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "foo-a",
						Image: "nginx:v1",
					},
					{
						Name:  "foo-b",
						Image: "nginx:v1",
					},
				},
			},
		}
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
				{
					TargetPolicy: appsv1alpha1.InjectAllContainers,
					PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
						Image: StringPoint("nginx:v2"),
					},
				},
			},
		})).Should(BeNil())
		Expect(pod.Spec.Containers[0].Image).Should(Equal("nginx:v2"))
		Expect(pod.Spec.Containers[1].Image).Should(Equal("nginx:v2"))
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
				{
					TargetPolicy: appsv1alpha1.InjectByName,
					PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
						Name:  StringPoint("foo-a"),
						Image: StringPoint("nginx:v1"),
					},
				},
			},
		})).Should(BeNil())
		Expect(pod.Spec.Containers[0].Image).Should(Equal("nginx:v1"))

		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
				{
					TargetPolicy: appsv1alpha1.InjectLastContainer,
					PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
						Image: StringPoint("nginx:v1"),
					},
				},
			},
		})).Should(BeNil())
		Expect(pod.Spec.Containers[1].Image).Should(Equal("nginx:v1"))

		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
				{
					TargetPolicy: appsv1alpha1.InjectFirstContainer,
					PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
						Image: StringPoint("nginx:v2"),
						Env: []v1.EnvVar{
							{
								Name:  "TEST_ENV",
								Value: "test",
							},
						},
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "volume-a",
								MountPath: "/usr/",
							},
						},
					},
				},
			},
		})).Should(BeNil())
		Expect(pod.Spec.Containers[0].Image).Should(Equal("nginx:v2"))
		Expect(pod.Spec.Containers[0].Env[0].Value).Should(Equal("test"))
		Expect(pod.Spec.Containers[0].VolumeMounts[0].Name).Should(Equal("volume-a"))
		runtimeClassName := "test"
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
				{
					TargetPolicy: appsv1alpha1.InjectFirstContainer,
					PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
						Image: StringPoint("nginx:v2"),
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "volume-a",
								MountPath: "/usr/local/",
							},
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "volume-a",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
				},
			},
			Affinity: &appsv1alpha1.PodDecorationAffinity{
				OverrideAffinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "test",
											Operator: v1.NodeSelectorOpExists,
										},
									},
								},
							},
						},
					},
				},
			},
			Tolerations: []v1.Toleration{
				{
					Key:      "test",
					Operator: v1.TolerationOpExists,
				},
			},
			RuntimeClassName: &runtimeClassName,
		})).Should(BeNil())
		Expect(pod.Spec.Containers[0].VolumeMounts[0].MountPath).Should(Equal("/usr/local/"))
	})
	It("patch Tolerations", func() {
		runtimeClassName := "test"
		pod := &v1.Pod{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "foo",
						Image: "nginx:v1",
					},
				},
			},
		}
		Expect(PatchPodDecoration(pod, &appsv1alpha1.PodDecorationPodTemplate{
			PrimaryContainers: []*appsv1alpha1.PrimaryContainerPatch{
				{
					TargetPolicy: appsv1alpha1.InjectFirstContainer,
					PodDecorationPrimaryContainer: appsv1alpha1.PodDecorationPrimaryContainer{
						Image: StringPoint("nginx:v2"),
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "volume-a",
								MountPath: "/usr/local/",
							},
						},
					},
				},
			},
			Volumes: []v1.Volume{
				{
					Name: "volume-a",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
				},
			},
			Affinity: &appsv1alpha1.PodDecorationAffinity{
				OverrideAffinity: &v1.Affinity{
					NodeAffinity: &v1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
							NodeSelectorTerms: []v1.NodeSelectorTerm{
								{
									MatchExpressions: []v1.NodeSelectorRequirement{
										{
											Key:      "test",
											Operator: v1.NodeSelectorOpExists,
										},
									},
								},
							},
						},
					},
				},
			},
			Tolerations: []v1.Toleration{
				{
					Key:      "test",
					Operator: v1.TolerationOpExists,
				},
			},
			RuntimeClassName: &runtimeClassName,
		})).Should(BeNil())
		Expect(pod.Spec.Tolerations[0].Key).Should(Equal("test"))
		Expect(pod.Spec.Affinity).ShouldNot(BeNil())
	})

	It("test anno utils", func() {
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					appsv1alpha1.AnnotationResourceDecorationRevision: "",
				},
				Labels: map[string]string{
					"app": "foo",
				},
			},
		}
		i0Int32 := int32(0)
		i1Int32 := int32(1)
		pdA := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pd-a",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-a",
					Weight: &i0Int32,
				},
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Metadata: []*appsv1alpha1.PodDecorationPodTemplateMeta{
						{
							Labels: map[string]string{"inj": "group-a"},
						},
					},
				},
			},
			Status: appsv1alpha1.PodDecorationStatus{
				UpdatedRevision: "100",
			},
		}
		pdB := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pd-b",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-b",
					Weight: &i1Int32,
				},
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Metadata: []*appsv1alpha1.PodDecorationPodTemplateMeta{
						{
							Labels: map[string]string{"inj": "group-b"},
						},
					},
				},
			},
			Status: appsv1alpha1.PodDecorationStatus{
				UpdatedRevision: "101",
			},
		}
		pdC := &appsv1alpha1.PodDecoration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pd-b",
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-b",
					Weight: &i1Int32,
				},
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
				Template: appsv1alpha1.PodDecorationPodTemplate{
					Metadata: []*appsv1alpha1.PodDecorationPodTemplateMeta{
						{
							Labels: map[string]string{"inj": "group-b-new"},
						},
					},
				},
			},
			Status: appsv1alpha1.PodDecorationStatus{
				UpdatedRevision: "102",
			},
		}
		pds := map[string]*appsv1alpha1.PodDecoration{
			"100": pdA,
			"101": pdB,
		}
		Expect(ShouldUpdateDecorationInfo(pod, pds)).Should(BeTrue())
		Expect(PatchListOfDecorations(pod, pds)).Should(BeNil())
		Expect(len(GetPodEffectiveDecorations(pod, []*appsv1alpha1.PodDecoration{pdA, pdC}, pds))).Should(Equal(2))
		Expect(GetDecorationGroupRevisionInfo(pod).Size()).Should(Equal(2))
		appsv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)
		_, _, err := GetPodDecorationsByPodAnno(context.TODO(), &mockClient{}, pod)
		Expect(err).ShouldNot(HaveOccurred())

		hav, err := GetHeaviestPDByGroup(context.TODO(), &mockClient{}, "", "")
		Expect(err).ShouldNot(HaveOccurred())
		Expect(hav.Name).Should(Equal("pd-d"))
	})
})

type mockClient struct {
	client.Client
}

func (*mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return nil
}

func (*mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	i0Int32 := int32(0)
	i1Int32 := int32(1)
	Tm1 := metav1.Now()
	Tm2 := metav1.NewTime(Tm1.Time.Add(100))
	list.(*appsv1alpha1.PodDecorationList).Items = []appsv1alpha1.PodDecoration{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "pd-a",
				CreationTimestamp: Tm1,
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-b",
					Weight: &i1Int32,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "pd-b",
				CreationTimestamp: Tm2,
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-b",
					Weight: &i1Int32,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "pd-c",
				CreationTimestamp: Tm1,
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-b",
					Weight: &i0Int32,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "pd-d",
				CreationTimestamp: Tm2,
			},
			Spec: appsv1alpha1.PodDecorationSpec{
				InjectStrategy: appsv1alpha1.PodDecorationInjectStrategy{
					Group:  "group-a",
					Weight: &i0Int32,
				},
			},
		},
	}
	return nil
}

var _ = BeforeSuite(func() {

})

func TestPodDecorationController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "test PodDecoration patch")
}

func StringPoint(str string) *string {
	return &str
}
