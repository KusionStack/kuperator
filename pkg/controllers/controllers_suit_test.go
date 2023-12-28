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

package controllers

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils/inject"
)

var (
	env    *envtest.Environment
	mgr    manager.Manager
	ctx    context.Context
	cancel context.CancelFunc
	c      client.Client
)

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	env = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
	}

	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(config).NotTo(BeNil())

	sch := scheme.Scheme
	Expect(appsv1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	Expect(appsv1alpha1.SchemeBuilder.AddToScheme(sch)).NotTo(HaveOccurred())
	mgr, err = manager.New(config, manager.Options{
		MetricsBindAddress: "0",
		NewCache:           inject.NewCacheWithFieldIndex,
	})
	Expect(err).NotTo(HaveOccurred())
	c = mgr.GetClient()
	Expect(AddToManager(mgr)).NotTo(HaveOccurred())

	go func() {
		err = mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	err := env.Stop()
	Expect(err).NotTo(HaveOccurred())
})
