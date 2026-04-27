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

package webhook

import (
	"context"
	"os"

	"k8s.io/klog/v2"
	certmanager "kusionstack.io/kube-utils/webhook/certmanager"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	mutatingWebhookConfigurationName   = "kusionstack-controller-manager-mutating"
	validatingWebhookConfigurationName = "kusionstack-controller-manager-validating"
	webhookCertsSecretName             = "webhook-certs"
)

// AddToManagerFuncs is a list of functions to add all Webhook Servers to the Manager
var AddToManagerFuncs []func(manager.Manager) error

// AddToManager adds all Webhook Servers to the Manager
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
func AddToManager(m manager.Manager) error {
	for _, f := range AddToManagerFuncs {
		if err := f(m); err != nil {
			return err
		}
	}
	return nil
}

// Initialize sets up the webhook certificate manager with auto-rotation support.
// It uses kube-utils/certmanager which:
// - Automatically generates and rotates certificates when expired
// - Syncs certs from Secret to local filesystem
// - Updates CABundle in WebhookConfigurations
func Initialize(ctx context.Context, mgr manager.Manager, dnsName string) error {
	cfg := certmanager.CertConfig{
		Host:                   dnsName,
		Namespace:              getNamespace(),
		SecretName:             webhookCertsSecretName,
		MutatingWebhookNames:   []string{mutatingWebhookConfigurationName},
		ValidatingWebhookNames: []string{validatingWebhookConfigurationName},
	}

	certMgr := certmanager.New(mgr, cfg)
	if err := certMgr.SetupWithManager(mgr); err != nil {
		return err
	}

	klog.InfoS("webhook cert manager initialized",
		"secret", webhookCertsSecretName,
		"namespace", getNamespace(),
		"host", dnsName)

	return nil
}

func getNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); len(ns) > 0 {
		return ns
	}
	return "kusionstack-system"
}
