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
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"
	"k8s.io/client-go/util/keyutil"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"kusionstack.io/kafed/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	mutatingWebhookConfigurationName   = "kusionstack-controller-manager-mutating"
	validatingWebhookConfigurationName = "kusionstack-controller-manager-validating"
	webhookCertsSecretName             = "kusionstack-webhook-certs"
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

func Initialize(ctx context.Context, config *rest.Config, dnsName, certDir string) error {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	return ensureWebhookCABundleAndCert(ctx, clientset, dnsName, certDir)
}

func ensureWebhookCABundleAndCert(ctx context.Context, clientset *kubernetes.Clientset, dnsName, certDir string) error {
	secret, err := ensureWebhookSecret(ctx, clientset, dnsName)
	if err != nil {
		return err
	}
	klog.Infof("webhook secret ensured, secret: %s", secret.Name)

	mwhc, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(ctx, mutatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for i := range mwhc.Webhooks {
		if mwhc.Webhooks[i].ClientConfig.CABundle == nil {
			mwhc.Webhooks[i].ClientConfig.CABundle = secret.Data["ca.crt"]
		}
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := clientset.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(ctx, mwhc, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}

	vwhc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, validatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for i := range vwhc.Webhooks {
		if vwhc.Webhooks[i].ClientConfig.CABundle == nil {
			vwhc.Webhooks[i].ClientConfig.CABundle = secret.Data["ca.crt"]
		}
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(ctx, vwhc, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return err
	}
	klog.Infof("webhook ca bundle ensured, mutatingwebhookconfiguration: %s, validatingwebhookconfiguration: %s", mutatingWebhookConfigurationName, validatingWebhookConfigurationName)

	var tlsKey, tlsCert []byte
	tlsKey, ok := secret.Data["tls.key"]
	if !ok {
		return errors.New("tls.key not found in secret")
	}
	tlsCert, ok = secret.Data["tls.crt"]
	if !ok {
		return errors.New("tls.crt not found in secret")
	}

	err = ensureWebhookCert(certDir, tlsKey, tlsCert)
	if err != nil {
		return err
	}
	klog.Infof("webhook cert ensured, cert dir: %s", certDir)

	return nil
}

func ensureWebhookSecret(ctx context.Context, clientset *kubernetes.Clientset, dnsName string) (secret *corev1.Secret, err error) {
	var (
		found = true
		dirty = false
	)
	secret, err = clientset.CoreV1().Secrets(getNamespace()).Get(ctx, webhookCertsSecretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			found = false
		} else {
			return
		}
	}
	if found {
		if secret.Data == nil || len(secret.Data) != 4 ||
			secret.Data["ca.key"] == nil || secret.Data["ca.crt"] == nil ||
			secret.Data["tls.key"] == nil || secret.Data["tls.crt"] == nil {
			dirty = true
		}
		if !dirty {
			return
		}
	}

	caKey, caCert, err := generateSelfSignedCACert()
	if err != nil {
		return
	}
	caKeyPEM, err := keyutil.MarshalPrivateKeyToPEM(caKey)
	if err != nil {
		return
	}
	caCertPEM := utils.EncodeCertPEM(caCert)

	privateKey, signedCert, err := generateSelfSignedCert(caCert, caKey, dnsName)
	if err != nil {
		return
	}
	privateKeyPEM, err := keyutil.MarshalPrivateKeyToPEM(privateKey)
	if err != nil {
		return
	}
	signedCertPEM := utils.EncodeCertPEM(signedCert)

	data := map[string][]byte{
		"ca.key": caKeyPEM, "ca.crt": caCertPEM,
		"tls.key": privateKeyPEM, "tls.crt": signedCertPEM,
	}
	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookCertsSecretName,
			Namespace: getNamespace(),
		},
		Data: data,
	}

	var updatedSecret *corev1.Secret
	err = retry.RetryOnConflict(retry.DefaultRetry, func() (err error) {
		if dirty {
			updatedSecret, err = clientset.CoreV1().Secrets(getNamespace()).Update(ctx, secret, metav1.UpdateOptions{})
		} else {
			updatedSecret, err = clientset.CoreV1().Secrets(getNamespace()).Create(ctx, secret, metav1.CreateOptions{})
		}
		return err
	})
	return updatedSecret, err
}

func generateSelfSignedCACert() (caKey *rsa.PrivateKey, caCert *x509.Certificate, err error) {
	caKey, err = utils.NewPrivateKey()
	if err != nil {
		return
	}

	caCert, err = cert.NewSelfSignedCACert(cert.Config{CommonName: "self-signed-k8s-cert"}, caKey)

	return
}

func generateSelfSignedCert(caCert *x509.Certificate, caKey crypto.Signer, dnsName string) (privateKey *rsa.PrivateKey, signedCert *x509.Certificate, err error) {
	privateKey, err = utils.NewPrivateKey()
	if err != nil {
		return
	}

	signedCert, err = utils.NewSignedCert(
		&cert.Config{
			CommonName: dnsName,
			AltNames:   cert.AltNames{DNSNames: []string{dnsName}},
			Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		},
		privateKey, caCert, caKey,
	)

	return
}

func ensureWebhookCert(certDir string, tlsKey, tlsCert []byte) error {
	if _, err := os.Stat(certDir); os.IsNotExist(err) {
		err := os.Mkdir(certDir, 0644)
		if err != nil {
			return err
		}
		klog.Infof("cert dir is created: %s", certDir)
	}

	keyFile := filepath.Join(certDir, "tls.key")
	certFile := filepath.Join(certDir, "tls.crt")

	if err := os.WriteFile(keyFile, tlsKey, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(certFile, tlsCert, 0644); err != nil {
		return err
	}
	return nil
}

func getNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); len(ns) > 0 {
		return ns
	}
	return "kusionstack-system"
}
