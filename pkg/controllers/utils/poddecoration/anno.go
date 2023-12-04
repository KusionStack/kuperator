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
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils"
)

type DecorationGroupRevisionInfo map[string]*DecorationInfo

type DecorationInfo struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

func (d DecorationGroupRevisionInfo) GetGroupPDRevision(group, rdName string) *string {
	info, ok := d[group]
	if ok && info.Name == rdName {
		return &info.Revision
	}
	return nil
}

func (d DecorationGroupRevisionInfo) GetCurrentPDNameByGroup(group string) *string {
	info, ok := d[group]
	if !ok {
		return nil
	}
	return &info.Name
}

func (d DecorationGroupRevisionInfo) Size() int {
	return len(d)
}

func GetDecorationGroupRevisionInfo(pod *corev1.Pod) (info DecorationGroupRevisionInfo) {
	info = DecorationGroupRevisionInfo{}
	if pod.Annotations == nil {
		return
	}
	val, ok := pod.Annotations[appsv1alpha1.AnnotationResourceDecorationRevision]
	if !ok {
		return
	}
	if err := json.Unmarshal([]byte(val), &info); err != nil {
		klog.Errorf("fail to unmarshal podDecoration anno on pod %s/%s, %v", pod.Namespace, pod.Name, err)
	}
	return
}

func setDecorationInfo(pod *corev1.Pod, podDecorations map[string]*appsv1alpha1.PodDecoration) {
	info := DecorationGroupRevisionInfo{}
	for revision, pd := range podDecorations {
		info[pd.Spec.InjectStrategy.Group] = &DecorationInfo{
			Name:     pd.Name,
			Revision: revision,
		}
	}
	byt, _ := json.Marshal(info)
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[appsv1alpha1.AnnotationResourceDecorationRevision] = string(byt)
}

func ShouldUpdateDecorationInfo(pod *corev1.Pod, podDecorations map[string]*appsv1alpha1.PodDecoration) bool {
	currentInfo := GetDecorationGroupRevisionInfo(pod)
	if currentInfo.Size() != len(podDecorations) {
		return true
	}
	for rv, pd := range podDecorations {
		revision := currentInfo.GetGroupPDRevision(pd.Spec.InjectStrategy.Group, pd.Name)
		if revision == nil || *revision != rv {
			return true
		}
	}
	return false
}

var PodDecorationCodec = scheme.Codecs.LegacyCodec(appsv1alpha1.GroupVersion)

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
	return clone, nil
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

func GetPodDecorationsByPodAnno(ctx context.Context, c client.Client, pod *corev1.Pod) (notFound bool, podDecorations map[string]*appsv1alpha1.PodDecoration, err error) {
	rdRevisions := getEffectivePodDecorationRevisionFromPod(pod)
	podDecorations = map[string]*appsv1alpha1.PodDecoration{}
	var revisions []*appsv1.ControllerRevision
	for _, revisionName := range rdRevisions {
		if len(revisionName) == 0 {
			continue
		}

		revision := &appsv1.ControllerRevision{}
		if err = c.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: revisionName}, revision); err != nil {
			if errors.IsNotFound(err) {
				klog.Errorf("fail to get PodDecoration revision %s for pod %s, [not found]: %v", revisionName, utils.ObjectKeyString(pod), err)
				notFound = true
				return
			}
			return false, podDecorations, fmt.Errorf("fail to get PodDecoration revision %s for pod %s: %v", revisionName, utils.ObjectKeyString(pod), err)
		}
		revisions = append(revisions, revision)
	}

	for _, revision := range revisions {
		pd, err := GetPodDecorationFromRevision(revision)
		if err != nil {
			return false, podDecorations, fmt.Errorf("fail to get PodDecoration revision %s for pod %s: %v", revision.Name, utils.ObjectKeyString(pod), err)
		}
		podDecorations[revision.Name] = pd
	}
	return
}

func getEffectivePodDecorationRevisionFromPod(pod *corev1.Pod) map[string]string {
	info := GetDecorationGroupRevisionInfo(pod)
	res := map[string]string{}
	for _, pdInfo := range info {
		res[pdInfo.Name] = pdInfo.Revision
	}
	return res
}
