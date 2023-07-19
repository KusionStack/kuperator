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

package admission

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	SidecarInjectPluginName = "SidecarInject"
)

var (
	DefaultOnPlugins = map[string]bool{
		SidecarInjectPluginName: true,
	}

	validateErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "webhook_validate_errors",
		Help: "Total number of validate errors per plugin",
	}, []string{"plugin"})

	validateTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "webhook_validate_seconds",
		Help: "Length of time per validate per plugin",
	}, []string{"plugin"})

	admitErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "webhook_admit_errors",
		Help: "Total number of admit errors per plugin",
	}, []string{"plugin"})

	admitTime = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "webhook_admit_seconds",
		Help: "Length of time per admit per plugin",
	}, []string{"plugin"})
)

func init() {
	metrics.Registry.MustRegister(
		validateErrors,
		validateTime,
		admitErrors,
		admitTime,
	)
}

type Plugin struct {
	name           string
	validationFunc ValidationFunc
	mutationFunc   MutationFunc
}

func NewPlugin(pluginName string, vf ValidationFunc, mf MutationFunc) PluginInterface {
	return &Plugin{
		name:           pluginName,
		validationFunc: vf,
		mutationFunc:   mf,
	}
}

func (p *Plugin) Name() string {
	return p.name
}

func (p *Plugin) Validate(ctx context.Context, req admission.Request, obj runtime.Object) error {
	if !DefaultOnPlugins[p.name] {
		return nil
	}
	if p.validationFunc == nil {
		return fmt.Errorf("plugin %s does not implement Validation", p.name)
	}

	startTime := time.Now()
	// Deep copy this object for each validating plugin, in case someone modifies it
	newObj := obj.DeepCopyObject()
	err := p.validationFunc(ctx, req, newObj)
	validateTime.WithLabelValues(p.name).Observe(time.Since(startTime).Seconds())

	if err != nil {
		validateErrors.WithLabelValues(p.name).Inc()
		err = fmt.Errorf("plugin %s validate for %s/%s failed: %v",
			p.name, req.AdmissionRequest.Namespace, req.AdmissionRequest.Name, err)
		klog.Warningf("%v, object: %v", err, obj)
	}

	// if object changed during validating, return error
	if !reflect.DeepEqual(obj, newObj) {
		validateErrors.WithLabelValues(p.name).Inc()
		err = fmt.Errorf("plugin %s validate for %s/%s failed: %v",
			p.name, req.AdmissionRequest.Namespace, req.AdmissionRequest.Name, err)
		klog.Warningf("%v, object: %v", err, obj)
	}

	return err
}

func (p *Plugin) Admit(ctx context.Context, req admission.Request, obj runtime.Object) error {
	if !DefaultOnPlugins[p.name] {
		return nil
	}
	if p.mutationFunc == nil {
		return fmt.Errorf("plugin %s does not implement Mutation", p.name)
	}

	startTime := time.Now()
	err := p.mutationFunc(ctx, req, obj)
	admitTime.WithLabelValues(p.name).Observe(time.Since(startTime).Seconds())

	if err != nil {
		admitErrors.WithLabelValues(p.name).Inc()
		err = fmt.Errorf("plugin %s admit for %s/%s failed: %v",
			p.name, req.AdmissionRequest.Namespace, req.AdmissionRequest.Name, err)
		klog.Warningf("%v, object: %v", err, obj)
	}
	return err
}
