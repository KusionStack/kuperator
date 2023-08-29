/**
 * Copyright 2023 The KusionStack Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package mixin

import (
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/recorder"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ inject.Cache = &ReconcilerMixin{}
var _ inject.APIReader = &ReconcilerMixin{}
var _ inject.Config = &ReconcilerMixin{}
var _ inject.Client = &ReconcilerMixin{}
var _ inject.Scheme = &ReconcilerMixin{}
var _ inject.Stoppable = &ReconcilerMixin{}
var _ inject.Mapper = &ReconcilerMixin{}
var _ inject.Logger = &ReconcilerMixin{}

type ReconcilerMixin struct {
	name string

	// inject by manager.SetFields
	Scheme     *runtime.Scheme
	Config     *rest.Config
	Client     client.Client
	APIReader  client.Reader
	Cache      cache.Cache
	Logger     logr.Logger
	StopCh     <-chan struct{}
	RESTMapper meta.RESTMapper

	// inject by recorder.Provider
	Recorder record.EventRecorder
}

func NewReconcilerMixin(controllerName string, mgr manager.Manager) *ReconcilerMixin {
	m := &ReconcilerMixin{
		name: controllerName,
	}

	// manager.SetFields injects following fields
	// - Config
	// - Client
	// - APIReader
	// - Scheme
	// - Cache
	// - Mapper
	// - StopChannel
	// - Logger
	mgr.SetFields(m)

	// additional inject fields
	m.InjectRecorder(mgr)
	return m
}

func (m *ReconcilerMixin) GetControllerName() string {
	return m.name
}

// InjectCache implements inject.Cache.
func (m *ReconcilerMixin) InjectCache(c cache.Cache) error {
	m.Cache = c
	return nil
}

// InjectAPIReader implements inject.APIReader.
func (m *ReconcilerMixin) InjectAPIReader(c client.Reader) error {
	m.APIReader = c
	return nil
}

// InjectConfig implements inject.Config.
func (m *ReconcilerMixin) InjectConfig(c *rest.Config) error {
	m.Config = c
	return nil
}

// InjectClient implements inject.Client.
func (m *ReconcilerMixin) InjectClient(c client.Client) error {
	m.Client = c
	return nil
}

// InjectScheme implements inject.Scheme.
func (m *ReconcilerMixin) InjectScheme(s *runtime.Scheme) error {
	m.Scheme = s
	return nil
}

// InjectStopChannel implements inject.Stoppable.
func (m *ReconcilerMixin) InjectStopChannel(stopCh <-chan struct{}) error {
	m.StopCh = stopCh
	return nil
}

// InjectMapper implements inject.Mapper.
func (m *ReconcilerMixin) InjectMapper(mapper meta.RESTMapper) error {
	m.RESTMapper = mapper
	return nil
}

// InjectLogger implements inject.Logger.
func (m *ReconcilerMixin) InjectLogger(l logr.Logger) error {
	m.Logger = l.WithName(m.name)
	return nil
}

func (m *ReconcilerMixin) InjectRecorder(p recorder.Provider) error {
	m.Recorder = p.GetEventRecorderFor(m.name)
	return nil
}

var _ inject.Client = &WebhookHandlerMixin{}
var _ inject.Logger = &WebhookHandlerMixin{}
var _ admission.DecoderInjector = &WebhookHandlerMixin{}

type WebhookHandlerMixin struct {
	Client  client.Client
	Decoder *admission.Decoder
	Logger  logr.Logger
}

func NewWebhookHandlerMixin() *WebhookHandlerMixin {
	return &WebhookHandlerMixin{}
}

// InjectDecoder implements admission.DecoderInjector.
func (m *WebhookHandlerMixin) InjectDecoder(d *admission.Decoder) error {
	m.Decoder = d
	return nil
}

// InjectLogger implements inject.Logger.
func (m *WebhookHandlerMixin) InjectLogger(l logr.Logger) error {
	m.Logger = l
	return nil
}

// InjectClient implements inject.Client.
func (m *WebhookHandlerMixin) InjectClient(c client.Client) error {
	m.Client = c
	return nil
}
