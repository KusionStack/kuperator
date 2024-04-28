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

package strategy

type sortedPodInfo struct {
	infos    []*podInfo
	revision string
}

func (p *sortedPodInfo) Len() int      { return len(p.infos) }
func (p *sortedPodInfo) Swap(i, j int) { p.infos[i], p.infos[j] = p.infos[j], p.infos[i] }
func (p *sortedPodInfo) Less(i, j int) bool {
	imatch := p.infos[i].revision == p.revision
	jmatch := p.infos[j].revision == p.revision
	if imatch != jmatch {
		return imatch
	}
	if p.infos[i].revision != p.infos[j].revision {
		// Move the latest version to the front,
		//  ensuring that the Pod selected by the partition is always ahead
		return p.infos[i].revision > p.infos[j].revision
	}
	// TODO: more sort method
	// Default sort by pod instance id in ResourceContext
	return p.infos[i].instanceId < p.infos[j].instanceId
}

type effectivePods map[string]*podInfo

type podInfo struct {
	name            string
	namespace       string
	labels          map[string]string
	collaSet        string
	resourceContext string
	instanceId      string
	revision        string
	isDeleted       bool
}

func (p *podInfo) InstanceKey() string {
	return p.resourceContext + "/" + p.instanceId
}
