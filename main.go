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

package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"

	kruisev1alpha1 "github.com/openkruise/kruise/apis/apps/v1alpha1"
	"github.com/spf13/pflag"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"kusionstack.io/operating/apis"
	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/controllers"
	"kusionstack.io/operating/pkg/features"
	"kusionstack.io/operating/pkg/utils/feature"
	"kusionstack.io/operating/pkg/utils/inject"
	"kusionstack.io/operating/pkg/webhook"

	_ "kusionstack.io/operating/pkg/features"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = clientgoscheme.Scheme
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(appsv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
		certDir              string
		dnsName              string
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&certDir, "cert-dir", webhookTempCertDir(), "The directory that contains the server key and certificate. If not set, webhook server would look up the server key and certificate in {TempDir}/k8s-webhook-server/serving-certs")
	flag.StringVar(&dnsName, "dns-name", "kusionstack-controller-manager.kusionstack-system.svc", "The DNS name of the webhook server.")

	klog.InitFlags(nil)
	defer klog.Flush()

	feature.DefaultMutableFeatureGate.AddFlag(pflag.CommandLine)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	ctrl.SetLogger(klogr.New())

	config := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "kusionstack-controller-manager",
		CertDir:                certDir,
		NewCache:               inject.NewCacheWithFieldIndex,
		Logger:                 ctrl.Log,

		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = apis.AddToScheme(mgr.GetScheme()); err != nil {
		setupLog.Error(err, "unable to add APIs scheme")
		os.Exit(1)
	}

	if feature.DefaultFeatureGate.Enabled(features.EnableKruiseToRecreate) {
		if err = kruisev1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
			setupLog.Error(err, "unable to add containerRecreateRequest API scheme")
			os.Exit(1)
		}
	}

	if err = controllers.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to add controller")
		os.Exit(1)
	}

	if err = webhook.AddToManager(mgr); err != nil {
		setupLog.Error(err, "unable to add webhook")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	setupLog.Info("initialize webhook")
	if err := webhook.Initialize(context.Background(), config, dnsName, certDir); err != nil {
		setupLog.Error(err, "unable to initialize webhook")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func webhookTempCertDir() string {
	return filepath.Join(os.TempDir(), "k8s-webhook-server", "serving-certs")
}
