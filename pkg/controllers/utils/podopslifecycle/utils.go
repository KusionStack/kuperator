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

package podopslifecycle

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kube-api/apps/v1alpha1"

	podopslifecycleutil "kusionstack.io/kuperator/pkg/controllers/podopslifecycle"
)

// IsDuringOps decides whether the Pod is during ops or not
// DuringOps means the Pod's OpsLifecycle phase is in or after PreCheck phase and before Finish phase.
func IsDuringOps(adapter LifecycleAdapter, obj client.Object) bool {
	_, hasID := checkOperatingID(adapter, obj)
	_, hasType := checkOperationType(adapter, obj)

	return hasID && hasType
}

// Begin is used for an CRD Operator to begin a lifecycle
func Begin(c client.Client, adapter LifecycleAdapter, obj client.Object, updateFunc ...UpdateFunc) (updated bool, err error) {
	if obj.GetLabels() == nil {
		obj.SetLabels(map[string]string{})
	}

	operatingID, hasID := checkOperatingID(adapter, obj)
	operationType, hasType := checkOperationType(adapter, obj)
	var needUpdate bool

	// ensure operatingID and operationType
	if hasID && hasType {
		if operationType != adapter.GetType() {
			err = fmt.Errorf("operatingID %s already has operationType %s", operatingID, operationType)
			return
		}
	} else {
		// check another id/type = this.type
		currentTypeIDs := queryByOperationType(adapter, obj)
		if currentTypeIDs != nil && currentTypeIDs.Len() > 0 && !adapter.AllowMultiType() {
			err = fmt.Errorf("operationType %s exists: %v", adapter.GetType(), currentTypeIDs)
			return
		}

		if !hasID {
			needUpdate = true
			setOperatingID(adapter, obj)
		}
		if !hasType {
			needUpdate = true
			setOperationType(adapter, obj)
		}
	}

	updated, err = DefaultUpdateAll(obj, append(updateFunc, adapter.WhenBegin)...)
	if err != nil {
		return
	}

	if needUpdate || updated {
		err = c.Update(context.Background(), obj)
		return err == nil, err
	}

	return false, nil
}

// BeginWithCleaningOld is used for an CRD Operator to begin a lifecycle with cleaning the old lifecycle
func BeginWithCleaningOld(c client.Client, adapter LifecycleAdapter, obj client.Object, updateFunc ...UpdateFunc) (updated bool, err error) {
	if podInUpdateLifecycle, err := podopslifecycleutil.IsLifecycleOnPod(adapter.GetID(), obj.(*corev1.Pod)); err != nil {
		return false, fmt.Errorf("fail to check %s PodOpsLifecycle on Pod %s/%s: %w", adapter.GetID(), obj.GetNamespace(), obj.GetName(), err)
	} else if podInUpdateLifecycle {
		if err := Undo(c, adapter, obj); err != nil {
			return false, err
		}
	}
	return Begin(c, adapter, obj, updateFunc...)
}

// AllowOps is used to check whether the PodOpsLifecycle phase is in UPGRADE to do following operations.
func AllowOps(adapter LifecycleAdapter, operationDelaySeconds int32, obj client.Object) (requeueAfter *time.Duration, allow bool) {
	if !IsDuringOps(adapter, obj) {
		return nil, false
	}

	startedTimestampStr, started := checkOperate(adapter, obj)
	if !started || operationDelaySeconds <= 0 {
		return nil, started
	}

	startedTimestamp, err := strconv.ParseInt(startedTimestampStr, 10, 64)
	if err != nil {
		return nil, started
	}

	startedTime := time.Unix(0, startedTimestamp)
	duration := time.Since(startedTime)
	delay := time.Duration(operationDelaySeconds) * time.Second
	if duration < delay {
		du := delay - duration
		return &du, started
	}

	return nil, started
}

// Finish is used for an CRD Operator to finish a lifecycle
func Finish(c client.Client, adapter LifecycleAdapter, obj client.Object, updateFunc ...UpdateFunc) (updated bool, err error) {
	operatingID, hasID := checkOperatingID(adapter, obj)
	operationType, hasType := checkOperationType(adapter, obj)

	if hasType && operationType != adapter.GetType() {
		return false, fmt.Errorf("operatingID %s has invalid operationType %s", operatingID, operationType)
	}

	var needUpdate bool
	if hasID || hasType {
		needUpdate = true
		deleteOperatingID(adapter, obj)
	}

	updated, err = DefaultUpdateAll(obj, append(updateFunc, adapter.WhenFinish)...)
	if err != nil {
		return
	}
	if needUpdate || updated {
		err = c.Update(context.Background(), obj)
		return err == nil, err
	}

	return false, err
}

// Undo is used for an CRD Operator to undo a lifecycle
func Undo(c client.Client, adapter LifecycleAdapter, obj client.Object) error {
	setUndo(adapter, obj)
	return c.Update(context.Background(), obj)
}

func checkOperatingID(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelID := fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, adapter.GetID())
	_, ok = obj.GetLabels()[labelID]
	return adapter.GetID(), ok
}

func checkOperationType(adapter LifecycleAdapter, obj client.Object) (val OperationType, ok bool) {
	labelType := fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, adapter.GetID())
	labelVal := obj.GetLabels()[labelType]
	val = OperationType(labelVal)
	return val, val == adapter.GetType()
}

func checkOperate(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelOperate := fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, adapter.GetID())
	val, ok = obj.GetLabels()[labelOperate]
	return
}

func setOperatingID(adapter LifecycleAdapter, obj client.Object) {
	labelID := fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelID] = fmt.Sprintf("%d", time.Now().UnixNano())
}

func setOperationType(adapter LifecycleAdapter, obj client.Object) {
	labelType := fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelType] = string(adapter.GetType())
}

// setOperate only for test
func setOperate(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelOperate := fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelOperate] = "true"
	return
}

func setUndo(adapter LifecycleAdapter, obj client.Object) {
	labelUndo := fmt.Sprintf("%s/%s", v1alpha1.PodUndoOperationTypeLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelUndo] = string(adapter.GetType())
}

func deleteOperatingID(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelID := fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, adapter.GetID())
	delete(obj.GetLabels(), labelID)
	return
}

func queryByOperationType(adapter LifecycleAdapter, obj client.Object) sets.String {
	res := sets.String{}
	valType := adapter.GetType()

	for k, v := range obj.GetLabels() {
		if strings.HasPrefix(k, v1alpha1.PodOperationTypeLabelPrefix) && v == string(valType) {
			res.Insert(k)
		}
	}

	return res
}

func DefaultUpdateAll(pod client.Object, updateFunc ...UpdateFunc) (updated bool, err error) {
	for _, f := range updateFunc {
		ok, updateErr := f(pod)
		if updateErr != nil {
			return updated, updateErr
		}
		updated = updated || ok
	}
	return updated, nil
}
