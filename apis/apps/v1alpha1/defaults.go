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

package v1alpha1

import (
	_ "crypto/sha256"
	_ "crypto/sha512"
	"fmt"
	"time"

	dockerref "github.com/docker/distribution/reference"
	corev1 "k8s.io/api/core/v1"
	utilpointer "k8s.io/utils/ptr"
)

const (
	DefaultImageTag = "latest"
)

func SetDefaults_PodSpec(podSpec *corev1.PodSpec) {
	SetDefaults_PodSpecConfig(podSpec)
	for i := range podSpec.Volumes {
		a := &podSpec.Volumes[i]
		SetDefaults_Volume(a)
		if a.VolumeSource.HostPath != nil {
			SetDefaults_HostPathVolumeSource(a.VolumeSource.HostPath)
		}
		if a.VolumeSource.Secret != nil {
			SetDefaults_SecretVolumeSource(a.VolumeSource.Secret)
		}
		if a.VolumeSource.ISCSI != nil {
			SetDefaults_ISCSIVolumeSource(a.VolumeSource.ISCSI)
		}
		if a.VolumeSource.RBD != nil {
			SetDefaults_RBDVolumeSource(a.VolumeSource.RBD)
		}
		if a.VolumeSource.DownwardAPI != nil {
			SetDefaults_DownwardAPIVolumeSource(a.VolumeSource.DownwardAPI)
			for j := range a.VolumeSource.DownwardAPI.Items {
				b := &a.VolumeSource.DownwardAPI.Items[j]
				if b.FieldRef != nil {
					SetDefaults_ObjectFieldSelector(b.FieldRef)
				}
			}
		}
		if a.VolumeSource.ConfigMap != nil {
			SetDefaults_ConfigMapVolumeSource(a.VolumeSource.ConfigMap)
		}
		if a.VolumeSource.AzureDisk != nil {
			SetDefaults_AzureDiskVolumeSource(a.VolumeSource.AzureDisk)
		}
		if a.VolumeSource.Projected != nil {
			SetDefaults_ProjectedVolumeSource(a.VolumeSource.Projected)
			for j := range a.VolumeSource.Projected.Sources {
				b := &a.VolumeSource.Projected.Sources[j]
				if b.DownwardAPI != nil {
					for k := range b.DownwardAPI.Items {
						c := &b.DownwardAPI.Items[k]
						if c.FieldRef != nil {
							SetDefaults_ObjectFieldSelector(c.FieldRef)
						}
					}
				}
				if b.ServiceAccountToken != nil {
					SetDefaults_ServiceAccountTokenProjection(b.ServiceAccountToken)
				}
			}
		}
		if a.VolumeSource.ScaleIO != nil {
			SetDefaults_ScaleIOVolumeSource(a.VolumeSource.ScaleIO)
		}
	}
	for i := range podSpec.InitContainers {
		a := &podSpec.InitContainers[i]
		SetDefaults_Container(a)
		for j := range a.Ports {
			b := &a.Ports[j]
			SetDefaults_ContainerPort(b)
		}
		for j := range a.Env {
			b := &a.Env[j]
			if b.ValueFrom != nil {
				if b.ValueFrom.FieldRef != nil {
					SetDefaults_ObjectFieldSelector(b.ValueFrom.FieldRef)
				}
			}
		}
		SetDefaults_ResourceList(&a.Resources.Limits)
		SetDefaults_ResourceList(&a.Resources.Requests)
		if a.LivenessProbe != nil {
			SetDefaults_Probe(a.LivenessProbe)
			if a.LivenessProbe.HTTPGet != nil {
				SetDefaults_HTTPGetAction(a.LivenessProbe.HTTPGet)
			}
		}
		if a.ReadinessProbe != nil {
			SetDefaults_Probe(a.ReadinessProbe)
			if a.ReadinessProbe.HTTPGet != nil {
				SetDefaults_HTTPGetAction(a.ReadinessProbe.HTTPGet)
			}
		}
		if a.Lifecycle != nil {
			if a.Lifecycle.PostStart != nil {
				if a.Lifecycle.PostStart.HTTPGet != nil {
					SetDefaults_HTTPGetAction(a.Lifecycle.PostStart.HTTPGet)
				}
			}
			if a.Lifecycle.PreStop != nil {
				if a.Lifecycle.PreStop.HTTPGet != nil {
					SetDefaults_HTTPGetAction(a.Lifecycle.PreStop.HTTPGet)
				}
			}
		}
	}
	for i := range podSpec.Containers {
		a := &podSpec.Containers[i]
		SetDefaults_Container(a)
		for j := range a.Ports {
			b := &a.Ports[j]
			SetDefaults_ContainerPort(b)
		}
		for j := range a.Env {
			b := &a.Env[j]
			if b.ValueFrom != nil {
				if b.ValueFrom.FieldRef != nil {
					SetDefaults_ObjectFieldSelector(b.ValueFrom.FieldRef)
				}
			}
		}
		SetDefaults_ResourceList(&a.Resources.Limits)
		SetDefaults_ResourceList(&a.Resources.Requests)
		if a.LivenessProbe != nil {
			SetDefaults_Probe(a.LivenessProbe)
			if a.LivenessProbe.HTTPGet != nil {
				SetDefaults_HTTPGetAction(a.LivenessProbe.HTTPGet)
			}
		}
		if a.ReadinessProbe != nil {
			SetDefaults_Probe(a.ReadinessProbe)
			if a.ReadinessProbe.HTTPGet != nil {
				SetDefaults_HTTPGetAction(a.ReadinessProbe.HTTPGet)
			}
		}
		if a.Lifecycle != nil {
			if a.Lifecycle.PostStart != nil {
				if a.Lifecycle.PostStart.HTTPGet != nil {
					SetDefaults_HTTPGetAction(a.Lifecycle.PostStart.HTTPGet)
				}
			}
			if a.Lifecycle.PreStop != nil {
				if a.Lifecycle.PreStop.HTTPGet != nil {
					SetDefaults_HTTPGetAction(a.Lifecycle.PreStop.HTTPGet)
				}
			}
		}
	}
}

func SetDefaults_PodSpecConfig(obj *corev1.PodSpec) {
	if obj.DNSPolicy == "" {
		obj.DNSPolicy = corev1.DNSClusterFirst
	}
	if obj.RestartPolicy == "" {
		obj.RestartPolicy = corev1.RestartPolicyAlways
	}
	if obj.HostNetwork {
		defaultHostNetworkPorts(&obj.Containers)
		defaultHostNetworkPorts(&obj.InitContainers)
	}
	if obj.SecurityContext == nil {
		obj.SecurityContext = &corev1.PodSecurityContext{}
	}
	if obj.TerminationGracePeriodSeconds == nil {
		period := int64(corev1.DefaultTerminationGracePeriodSeconds)
		obj.TerminationGracePeriodSeconds = &period
	}
	if obj.SchedulerName == "" {
		obj.SchedulerName = corev1.DefaultSchedulerName
	}
}

// With host networking default all container ports to host ports.
func defaultHostNetworkPorts(containers *[]corev1.Container) {
	for i := range *containers {
		for j := range (*containers)[i].Ports {
			if (*containers)[i].Ports[j].HostPort == 0 {
				(*containers)[i].Ports[j].HostPort = (*containers)[i].Ports[j].ContainerPort
			}
		}
	}
}

func SetDefaults_Volume(obj *corev1.Volume) {
	if utilpointer.AllPtrFieldsNil(&obj.VolumeSource) {
		obj.VolumeSource = corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}
}
func SetDefaults_Probe(obj *corev1.Probe) {
	if obj.TimeoutSeconds == 0 {
		obj.TimeoutSeconds = 1
	}
	if obj.PeriodSeconds == 0 {
		obj.PeriodSeconds = 10
	}
	if obj.SuccessThreshold == 0 {
		obj.SuccessThreshold = 1
	}
	if obj.FailureThreshold == 0 {
		obj.FailureThreshold = 3
	}
}
func SetDefaults_SecretVolumeSource(obj *corev1.SecretVolumeSource) {
	if obj.DefaultMode == nil {
		perm := int32(corev1.SecretVolumeSourceDefaultMode)
		obj.DefaultMode = &perm
	}
}
func SetDefaults_ConfigMapVolumeSource(obj *corev1.ConfigMapVolumeSource) {
	if obj.DefaultMode == nil {
		perm := int32(corev1.ConfigMapVolumeSourceDefaultMode)
		obj.DefaultMode = &perm
	}
}
func SetDefaults_DownwardAPIVolumeSource(obj *corev1.DownwardAPIVolumeSource) {
	if obj.DefaultMode == nil {
		perm := int32(corev1.DownwardAPIVolumeSourceDefaultMode)
		obj.DefaultMode = &perm
	}
}
func SetDefaults_Secret(obj *corev1.Secret) {
	if obj.Type == "" {
		obj.Type = corev1.SecretTypeOpaque
	}
}
func SetDefaults_ProjectedVolumeSource(obj *corev1.ProjectedVolumeSource) {
	if obj.DefaultMode == nil {
		perm := int32(corev1.ProjectedVolumeSourceDefaultMode)
		obj.DefaultMode = &perm
	}
}
func SetDefaults_ServiceAccountTokenProjection(obj *corev1.ServiceAccountTokenProjection) {
	hour := int64(time.Hour.Seconds())
	if obj.ExpirationSeconds == nil {
		obj.ExpirationSeconds = &hour
	}
}
func SetDefaults_ISCSIVolumeSource(obj *corev1.ISCSIVolumeSource) {
	if obj.ISCSIInterface == "" {
		obj.ISCSIInterface = "default"
	}
}
func SetDefaults_ISCSIPersistentVolumeSource(obj *corev1.ISCSIPersistentVolumeSource) {
	if obj.ISCSIInterface == "" {
		obj.ISCSIInterface = "default"
	}
}
func SetDefaults_AzureDiskVolumeSource(obj *corev1.AzureDiskVolumeSource) {
	if obj.CachingMode == nil {
		obj.CachingMode = new(corev1.AzureDataDiskCachingMode)
		*obj.CachingMode = corev1.AzureDataDiskCachingReadWrite
	}
	if obj.Kind == nil {
		obj.Kind = new(corev1.AzureDataDiskKind)
		*obj.Kind = corev1.AzureSharedBlobDisk
	}
	if obj.FSType == nil {
		obj.FSType = new(string)
		*obj.FSType = "ext4"
	}
	if obj.ReadOnly == nil {
		obj.ReadOnly = new(bool)
		*obj.ReadOnly = false
	}
}
func SetDefaults_Endpoints(obj *corev1.Endpoints) {
	for i := range obj.Subsets {
		ss := &obj.Subsets[i]
		for i := range ss.Ports {
			ep := &ss.Ports[i]
			if ep.Protocol == "" {
				ep.Protocol = corev1.ProtocolTCP
			}
		}
	}
}
func SetDefaults_HTTPGetAction(obj *corev1.HTTPGetAction) {
	if obj.Path == "" {
		obj.Path = "/"
	}
	if obj.Scheme == "" {
		obj.Scheme = corev1.URISchemeHTTP
	}
}
func SetDefaults_NamespaceStatus(obj *corev1.NamespaceStatus) {
	if obj.Phase == "" {
		obj.Phase = corev1.NamespaceActive
	}
}
func SetDefaults_ObjectFieldSelector(obj *corev1.ObjectFieldSelector) {
	if obj.APIVersion == "" {
		obj.APIVersion = "v1"
	}
}
func SetDefaults_LimitRangeItem(obj *corev1.LimitRangeItem) {
	// for container limits, we apply default values
	if obj.Type == corev1.LimitTypeContainer {

		if obj.Default == nil {
			obj.Default = make(corev1.ResourceList)
		}
		if obj.DefaultRequest == nil {
			obj.DefaultRequest = make(corev1.ResourceList)
		}

		// If a default limit is unspecified, but the max is specified, default the limit to the max
		for key, value := range obj.Max {
			if _, exists := obj.Default[key]; !exists {
				obj.Default[key] = value.DeepCopy()
			}
		}
		// If a default limit is specified, but the default request is not, default request to limit
		for key, value := range obj.Default {
			if _, exists := obj.DefaultRequest[key]; !exists {
				obj.DefaultRequest[key] = value.DeepCopy()
			}
		}
		// If a default request is not specified, but the min is provided, default request to the min
		for key, value := range obj.Min {
			if _, exists := obj.DefaultRequest[key]; !exists {
				obj.DefaultRequest[key] = value.DeepCopy()
			}
		}
	}
}
func SetDefaults_ConfigMap(obj *corev1.ConfigMap) {
	if obj.Data == nil {
		obj.Data = make(map[string]string)
	}
}

func SetDefaults_RBDVolumeSource(obj *corev1.RBDVolumeSource) {
	if obj.RBDPool == "" {
		obj.RBDPool = "rbd"
	}
	if obj.RadosUser == "" {
		obj.RadosUser = "admin"
	}
	if obj.Keyring == "" {
		obj.Keyring = "/etc/ceph/keyring"
	}
}

func SetDefaults_RBDPersistentVolumeSource(obj *corev1.RBDPersistentVolumeSource) {
	if obj.RBDPool == "" {
		obj.RBDPool = "rbd"
	}
	if obj.RadosUser == "" {
		obj.RadosUser = "admin"
	}
	if obj.Keyring == "" {
		obj.Keyring = "/etc/ceph/keyring"
	}
}

func SetDefaults_ScaleIOVolumeSource(obj *corev1.ScaleIOVolumeSource) {
	if obj.StorageMode == "" {
		obj.StorageMode = "ThinProvisioned"
	}
	if obj.FSType == "" {
		obj.FSType = "xfs"
	}
}

func SetDefaults_ScaleIOPersistentVolumeSource(obj *corev1.ScaleIOPersistentVolumeSource) {
	if obj.StorageMode == "" {
		obj.StorageMode = "ThinProvisioned"
	}
	if obj.FSType == "" {
		obj.FSType = "xfs"
	}
}

func SetDefaults_HostPathVolumeSource(obj *corev1.HostPathVolumeSource) {
	typeVol := corev1.HostPathUnset
	if obj.Type == nil {
		obj.Type = &typeVol
	}
}

func SetDefaults_Container(obj *corev1.Container) {
	if obj.ImagePullPolicy == "" {
		// Ignore error and assume it has been validated elsewhere
		_, tag, _, _ := ParseImageName(obj.Image)

		// Check image tag
		if tag == "latest" {
			obj.ImagePullPolicy = corev1.PullAlways
		} else {
			obj.ImagePullPolicy = corev1.PullIfNotPresent
		}
	}
	if obj.TerminationMessagePath == "" {
		obj.TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
	if obj.TerminationMessagePolicy == "" {
		obj.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
}

func SetDefaults_ContainerPort(obj *corev1.ContainerPort) {
	if obj.Protocol == "" {
		obj.Protocol = corev1.ProtocolTCP
	}
}

func SetDefaults_ResourceList(obj *corev1.ResourceList) {
	for key, val := range *obj {
		// TODO(#18538): We round up resource values to milli scale to maintain API compatibility.
		// In the future, we should instead reject values that need rounding.
		const milliScale = -3
		val.RoundUp(milliScale)

		(*obj)[corev1.ResourceName(key)] = val
	}
}

// ParseImageName parses a docker image string into three parts: repo, tag and digest.
// If both tag and digest are empty, a default image tag will be returned.
func ParseImageName(image string) (string, string, string, error) {
	named, err := dockerref.ParseNormalizedNamed(image)
	if err != nil {
		return "", "", "", fmt.Errorf("couldn't parse image name: %v", err)
	}

	repoToPull := named.Name()
	var tag, digest string

	tagged, ok := named.(dockerref.Tagged)
	if ok {
		tag = tagged.Tag()
	}

	digested, ok := named.(dockerref.Digested)
	if ok {
		digest = digested.Digest().String()
	}
	// If no tag was specified, use the default "latest".
	if len(tag) == 0 && len(digest) == 0 {
		tag = DefaultImageTag
	}
	return repoToPull, tag, digest, nil
}
