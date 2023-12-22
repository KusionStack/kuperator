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

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	"kusionstack.io/operating/pkg/utils/feature"
)

const (
	// AlibabaCloudSlb enables the alibaba_cloud_slb controller.
	AlibabaCloudSlb featuregate.Feature = "AlibabaCloudSlb"
	// GraceDeleteWebhook enables the gracedelete webhook
	GraceDeleteWebhook featuregate.Feature = "GraceDeleteWebhook"
)

var defaultFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	AlibabaCloudSlb:    {Default: false, PreRelease: featuregate.Alpha},
	GraceDeleteWebhook: {Default: false, PreRelease: featuregate.Alpha},
}

func init() {
	runtime.Must(feature.DefaultMutableFeatureGate.Add(defaultFeatureGates))
}
