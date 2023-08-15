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
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/apis/apps/v1alpha1"
)

// IsDuringOps decides whether the Pod is during ops or not
// DuringOps means the Pod's OpsLifecycle phase is in or after PreCheck phase and before Finish phase.
func IsDuringOps(adapter LifecycleAdapter, obj client.Object) bool {
	_, hasID := checkOperatingID(adapter, obj)
	_, hasType := checkOperationType(adapter, obj)

	return hasID && hasType
}

// Begin is used for an CRD Operator to begin a lifecycle
func Begin(c client.Client, adapter LifecycleAdapter, obj client.Object) (updated bool, err error) {
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

	updated, err = adapter.WhenBegin(obj)
	if err != nil {
		return
	}

	if needUpdate || updated {
		err = c.Update(context.Background(), obj)
		return true, err
	}

	return false, nil
}

// AllowOps is used to check whether the PodOpsLifecycle phase is in UPGRADE to do following operations.
func AllowOps(adapter LifecycleAdapter, obj client.Object) (allow bool) {
	if !IsDuringOps(adapter, obj) {
		return false
	}

	_, started := checkOperate(adapter, obj)
	return started
}

// Finish is used for an CRD Operator to finish a lifecycle
func Finish(c client.Client, adapter LifecycleAdapter, obj client.Object) (updated bool, err error) {
	operatingID, hasID := checkOperatingID(adapter, obj)
	operationType, hasType := checkOperationType(adapter, obj)

	if hasType && operationType != adapter.GetType() {
		return false, fmt.Errorf("operatingID %s has invalid operationType %s", operatingID, operationType)
	}

	var needUpdate bool
	if hasID || hasType {
		needUpdate = true
		deleteOperatingID(adapter, obj)
		deleteOperationType(adapter, obj)
	}

	updated, err = adapter.WhenFinish(obj)
	if err != nil {
		return
	}
	if needUpdate || updated {
		err = c.Update(context.Background(), obj)
		if err != nil {
			return
		}
	}

	return needUpdate, err
}

func checkOperatingID(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelID := fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, adapter.GetID())
	_, ok = obj.GetLabels()[labelID]
	return adapter.GetID(), ok
}

func checkOperationType(adapter LifecycleAdapter, obj client.Object) (val OperationType, ok bool) {
	labelType := fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, adapter.GetID())

	var labelVal string
	labelVal, ok = obj.GetLabels()[labelType]
	val = OperationType(labelVal)

	return
}

func checkOperate(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelOperate := fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, adapter.GetID())
	val, ok = obj.GetLabels()[labelOperate]
	return
}

func setOperatingID(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelID := fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelID] = fmt.Sprintf("%d", time.Now().UnixNano())
	return
}

func setOperationType(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelType := fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelType] = string(adapter.GetType())
	return
}

// setOperate only for test
func setOperate(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelOperate := fmt.Sprintf("%s/%s", v1alpha1.PodOperateLabelPrefix, adapter.GetID())
	obj.GetLabels()[labelOperate] = "true"
	return
}

func deleteOperatingID(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelID := fmt.Sprintf("%s/%s", v1alpha1.PodOperatingLabelPrefix, adapter.GetID())
	delete(obj.GetLabels(), labelID)
	return
}

func deleteOperationType(adapter LifecycleAdapter, obj client.Object) (val string, ok bool) {
	labelType := fmt.Sprintf("%s/%s", v1alpha1.PodOperationTypeLabelPrefix, adapter.GetID())
	delete(obj.GetLabels(), labelType)
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
