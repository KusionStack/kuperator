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

package opscore

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/operating/pkg/controllers/utils/podopslifecycle"
)

type GetID func() string
type GetType func() podopslifecycle.OperationType
type AllowMultiType func() bool
type WhenBegin func(client.Object) (bool, error)
type WhenFinish func(client.Object) (bool, error)

type GenericLifecycleAdapter struct {
	getID          GetID
	getType        GetType
	allowMultiType AllowMultiType
	whenBegin      WhenBegin
	whenFinish     WhenFinish
}

func (g GenericLifecycleAdapter) GetID() string {
	return g.getID()
}

func (g GenericLifecycleAdapter) GetType() podopslifecycle.OperationType {
	return g.getType()
}

func (g GenericLifecycleAdapter) AllowMultiType() bool {
	return g.allowMultiType()
}

func (g GenericLifecycleAdapter) WhenBegin(pod client.Object) (bool, error) {
	return g.whenBegin(pod)
}

func (g GenericLifecycleAdapter) WhenFinish(pod client.Object) (bool, error) {
	return g.whenFinish(pod)
}

func NewLifecycleAdapter(action string) podopslifecycle.LifecycleAdapter {
	getID := func() string {
		return "operationjob"
	}
	getType := func() podopslifecycle.OperationType {
		return podopslifecycle.OperationType(action)
	}
	allowMultiType := func() bool {
		return true
	}
	whenBegin := func(_ client.Object) (bool, error) {
		return false, nil
	}
	whenFinish := func(_ client.Object) (bool, error) {
		return false, nil
	}

	return &GenericLifecycleAdapter{
		getID:          getID,
		getType:        getType,
		allowMultiType: allowMultiType,
		whenBegin:      whenBegin,
		whenFinish:     whenFinish,
	}
}
