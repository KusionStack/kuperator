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

package anno

import (
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	appsv1alpha1 "kusionstack.io/kube-api/apps/v1alpha1"
)

type DecorationRevisionInfo []*DecorationInfo

type DecorationInfo struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

func (d DecorationRevisionInfo) GetRevision(name string) *string {
	for _, info := range d {
		if info.Name == name {
			return &info.Revision
		}
	}
	return nil
}

func (d DecorationRevisionInfo) Size() int {
	return len(d)
}

func GetDecorationRevisionInfo(pod *corev1.Pod) (info DecorationRevisionInfo) {
	info = DecorationRevisionInfo{}
	if pod.Annotations == nil {
		return
	}
	val, ok := pod.Annotations[appsv1alpha1.AnnotationPodDecorationRevision]
	if !ok {
		return
	}
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		klog.Errorf("fail to unmarshal podDecoration anno on pod %s/%s, %v", pod.Namespace, pod.Name, err)
	}
	return
}

func CurrentRevision(pod *corev1.Pod, podDecorationName string) *string {
	if pod.Labels != nil {
		val, ok := pod.Labels[appsv1alpha1.PodDecorationLabelPrefix+podDecorationName]
		if ok {
			return &val
		}
	}
	return nil
}

func SetDecorationInfo(pod *corev1.Pod, podDecorations map[string]*appsv1alpha1.PodDecoration) {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Annotations[appsv1alpha1.AnnotationPodDecorationRevision] = GetDecorationInfoString(podDecorations)
	for revision, pd := range podDecorations {
		pod.Labels[appsv1alpha1.PodDecorationLabelPrefix+pd.Name] = revision
	}
}

func GetDecorationInfoString(podDecorations map[string]*appsv1alpha1.PodDecoration) string {
	info := DecorationRevisionInfo{}
	for revision, pd := range podDecorations {
		info = append(info, &DecorationInfo{
			Name:     pd.Name,
			Revision: revision,
		})
	}
	byt, _ := json.Marshal(info)
	return string(byt)
}

func UnmarshallFromString(val string) ([]*DecorationInfo, error) {
	info := DecorationRevisionInfo{}
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		return nil, err
	}
	return info, nil
}

var PodDecorationCodec = scheme.Codecs.LegacyCodec(appsv1alpha1.SchemeGroupVersion)

func ApplyPatch(revision *appsv1.ControllerRevision) (*appsv1alpha1.PodDecoration, error) {
	clone := &appsv1alpha1.PodDecoration{}
	patched, err := strategicpatch.StrategicMergePatch([]byte(runtime.EncodeOrDie(PodDecorationCodec, clone)), revision.Data.Raw, clone)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(patched, clone)
	if err != nil {
		return nil, err
	}
	return copyMetadataFromRevOwnerRef(clone, revision), nil
}

func copyMetadataFromRevOwnerRef(pd *appsv1alpha1.PodDecoration, rev *appsv1.ControllerRevision) (retPD *appsv1alpha1.PodDecoration) {
	retPD = pd
	if rev == nil || len(rev.OwnerReferences) == 0 {
		return
	}
	for _, ownerRef := range rev.OwnerReferences {
		if *ownerRef.Controller == true && ownerRef.Kind == "PodDecoration" {
			retPD.APIVersion = ownerRef.APIVersion
			retPD.UID = ownerRef.UID
			return
		}
	}
	return
}

func GetPodDecorationFromRevision(revision *appsv1.ControllerRevision) (*appsv1alpha1.PodDecoration, error) {
	podDecoration, err := ApplyPatch(revision)
	if err != nil {
		return nil, fmt.Errorf("fail to get ResourceDecoration from revision %s/%s: %s", revision.Namespace, revision.Name, err)
	}

	podDecoration.Namespace = revision.Namespace
	for _, ownerRef := range revision.OwnerReferences {
		if ownerRef.Controller != nil && *ownerRef.Controller {
			podDecoration.Name = ownerRef.Name
			break
		}
		podDecoration.Name = ownerRef.Name
	}
	return podDecoration, nil
}
