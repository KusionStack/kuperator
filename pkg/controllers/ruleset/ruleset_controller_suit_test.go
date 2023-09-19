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

package ruleset

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1alpha1 "kusionstack.io/operating/apis/apps/v1alpha1"
	"kusionstack.io/operating/pkg/utils/inject"
)

var (
	env     *envtest.Environment
	mgr     manager.Manager
	c       client.Client
	request chan reconcile.Request

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
)

func TestMain(m *testing.M) {
	ctx, cancel = context.WithCancel(context.TODO())
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	appsv1alpha1.SchemeBuilder.AddToScheme(scheme.Scheme)

	env = &envtest.Environment{
		Scheme:            scheme.Scheme,
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}

	initRulesetManager()

	config, err := env.Start()
	if err != nil {
		panic(err)
	}
	wg.Add(1)
	go func() {
		mgr, err = manager.New(config, manager.Options{
			MetricsBindAddress: "0",
			NewCache:           inject.NewCacheWithFieldIndex,
		})
		var r reconcile.Reconciler
		r, request = testReconcile(newReconciler(mgr))
		_, err = addToMgr(mgr, r)

		c = mgr.GetClient()

		defer wg.Done()
		err = mgr.Start(ctx)
		if err != nil {
			panic(err)
		}
	}()
	<-time.After(time.Second * 5)
	code := m.Run()
	env.Stop()
	os.Exit(code)
}

func testReconcile(inner reconcile.Reconciler) (reconcile.Reconciler, chan reconcile.Request) {
	requests := make(chan reconcile.Request, 5)
	fn := reconcile.Func(func(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
		result, err := inner.Reconcile(ctx, req)
		if _, done := ctx.Deadline(); !done && len(requests) == 0 {
			requests <- req
		}
		return result, err
	})
	return fn, requests
}

func Stop() {
	cancel()
	wg.Wait()
}
