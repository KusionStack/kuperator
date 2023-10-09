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

package resourceconsist

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	errors2 "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"kusionstack.io/operating/apis/apps/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/operating/pkg/controllers/utils"
)

func (r *Consist) syncEmployer(ctx context.Context, employer client.Object, expectEmployerStatus, currentEmployerStatus []IEmployer) (bool, bool, error) {
	toCudEmployer, err := r.diffEmployer(expectEmployerStatus, currentEmployerStatus)
	if err != nil {
		return false, false, fmt.Errorf("diff employer failed, err: %s", err.Error())
	}
	_, failCreate, err := r.adapter.CreateEmployer(ctx, employer, toCudEmployer.ToCreate)
	if err != nil {
		return false, false, fmt.Errorf("syncCreate failed, err: %s", err.Error())
	}
	_, failUpdate, err := r.adapter.UpdateEmployer(ctx, employer, toCudEmployer.ToUpdate)
	if err != nil {
		return false, false, fmt.Errorf("syncUpdate failed, err: %s", err.Error())
	}
	_, failDelete, err := r.adapter.DeleteEmployer(ctx, employer, toCudEmployer.ToDelete)
	if err != nil {
		return false, false, fmt.Errorf("syncDelete failed, err: %s", err.Error())
	}

	isClean := len(toCudEmployer.Unchanged) == 0 && len(toCudEmployer.ToCreate) == 0 && len(toCudEmployer.ToUpdate) == 0 && len(failDelete) == 0
	cudFailedExist := len(failCreate) > 0 || len(failUpdate) > 0 || len(failDelete) > 0
	return isClean, cudFailedExist, nil
}

func (r *Consist) diffEmployer(expectEmployer, currentEmployer []IEmployer) (ToCUDEmployer, error) {
	expectEmployerMap := make(map[string]IEmployer)
	currentEmployerMap := make(map[string]IEmployer)

	for _, expect := range expectEmployer {
		expectEmployerMap[expect.GetEmployerId()] = expect
	}
	for _, current := range currentEmployer {
		currentEmployerMap[current.GetEmployerId()] = current
	}

	toCreate := make([]IEmployer, len(expectEmployer))
	toUpdate := make([]IEmployer, len(currentEmployer))
	toDelete := make([]IEmployer, len(currentEmployer))
	unchanged := make([]IEmployer, len(currentEmployer))
	toCreateIdx, toUpdateIdx, toDeleteIdx, unchangedIdx := 0, 0, 0, 0

	for expectId, expect := range expectEmployerMap {
		current, exist := currentEmployerMap[expectId]
		if !exist {
			toCreate[toCreateIdx] = expect
			toCreateIdx++
			continue
		}
		equal, err := expect.EmployerEqual(current)
		if err != nil {
			return ToCUDEmployer{}, err
		}
		if !equal {
			toUpdate[toUpdateIdx] = expect
			toUpdateIdx++
			continue
		}
		unchanged[unchangedIdx] = expect
		unchangedIdx++
	}

	for currentId, current := range currentEmployerMap {
		_, exist := expectEmployerMap[currentId]
		if !exist {
			toDelete[toDeleteIdx] = current
			toDeleteIdx++
		}
	}

	// todo, log level
	r.Logger.Info("employer info",
		"toCreate", toCreate[:toCreateIdx],
		"toUpdate", toUpdate[:toUpdateIdx],
		"toDelete", toDelete[:toDeleteIdx],
		"unchanged", unchanged[:unchangedIdx],
	)

	return ToCUDEmployer{
		ToCreate:  toCreate[:toCreateIdx],
		ToUpdate:  toUpdate[:toUpdateIdx],
		ToDelete:  toDelete[:toDeleteIdx],
		Unchanged: unchanged[:unchangedIdx],
	}, nil
}

func (r *Consist) diffEmployees(expectEmployees, currentEmployees []IEmployee) (ToCUDEmployees, error) {
	expectEmployeesMap := make(map[string]IEmployee)
	currentEmployeesMap := make(map[string]IEmployee)

	for _, expect := range expectEmployees {
		expectEmployeesMap[expect.GetEmployeeId()] = expect
	}
	for _, current := range currentEmployees {
		currentEmployeesMap[current.GetEmployeeId()] = current
	}

	toCreate := make([]IEmployee, len(expectEmployees))
	toUpdate := make([]IEmployee, len(currentEmployees))
	toDelete := make([]IEmployee, len(currentEmployees))
	unchanged := make([]IEmployee, len(currentEmployees))
	toCreateIdx, toUpdateIdx, toDeleteIdx, unchangedIdx := 0, 0, 0, 0

	for expectId, expect := range expectEmployeesMap {
		current, exist := currentEmployeesMap[expectId]
		if !exist {
			toCreate[toCreateIdx] = expect
			toCreateIdx++
			continue
		}
		equal, err := expect.EmployeeEqual(current)
		if err != nil {
			return ToCUDEmployees{}, err
		}
		if !equal {
			toUpdate[toUpdateIdx] = expect
			toUpdateIdx++
			continue
		}
		unchanged[unchangedIdx] = expect
		unchangedIdx++
	}

	for currentId, current := range currentEmployeesMap {
		_, exist := expectEmployeesMap[currentId]
		if !exist {
			toDelete[toDeleteIdx] = current
			toDeleteIdx++
		}
	}

	// todo, log level
	r.Logger.Info("employee info",
		"toCreate", toCreate[:toCreateIdx],
		"toUpdate", toUpdate[:toUpdateIdx],
		"toDelete", toDelete[:toDeleteIdx],
		"unchanged", unchanged[:unchangedIdx],
	)

	return ToCUDEmployees{
		ToCreate:  toCreate[:toCreateIdx],
		ToUpdate:  toUpdate[:toUpdateIdx],
		ToDelete:  toDelete[:toDeleteIdx],
		Unchanged: unchanged[:unchangedIdx],
	}, nil
}

func (r *Consist) syncEmployees(ctx context.Context, employer client.Object, expectEmployees, currentEmployees []IEmployee) (bool, bool, error) {
	// get expect/current employees diffEmployees
	toCudEmployees, err := r.diffEmployees(expectEmployees, currentEmployees)
	if err != nil {
		return false, false, err
	}

	succCreate, failCreate, err := r.adapter.CreateEmployees(ctx, employer, toCudEmployees.ToCreate)
	if err != nil {
		return false, false, fmt.Errorf("syncCreate failed, err: %s", err.Error())
	}
	succUpdate, failUpdate, err := r.adapter.UpdateEmployees(ctx, employer, toCudEmployees.ToUpdate)
	if err != nil {
		return false, false, fmt.Errorf("syncUpdate failed, err: %s", err.Error())
	}
	succDelete, failDelete, err := r.adapter.DeleteEmployees(ctx, employer, toCudEmployees.ToDelete)
	if err != nil {
		return false, false, fmt.Errorf("syncDelete failed, err: %s", err.Error())
	}

	toAddLifecycleFlzEmployees, toDeleteLifecycleFlzEmployees := r.getToAddDeleteLifecycleFlzEmployees(
		succCreate, succDelete, succUpdate, toCudEmployees.Unchanged)

	recordedNotSelected := make(map[string]bool)
	lifecycleOptions, lifecycleOptionsImplemented := r.adapter.(ReconcileLifecycleOptions)
	needRecordEmployees := lifecycleOptionsImplemented && lifecycleOptions.FollowPodOpsLifeCycle() && lifecycleOptions.NeedRecordEmployees()
	if needRecordEmployees {
		if employer.GetAnnotations()[lifecycleFinalizerRecordedAnnoKey] != "" {
			selectedEmployees, err := r.adapter.GetSelectedEmployeeNames(ctx, employer)
			if err != nil {
				return false, false, fmt.Errorf("GetSelectedEmployeeNames failed, err: %s", err.Error())
			}
			recordedEmployees := strings.Split(employer.GetAnnotations()[lifecycleFinalizerRecordedAnnoKey], ",")
			selectedSet := sets.NewString(selectedEmployees...)
			for _, recordedEmployee := range recordedEmployees {
				if !selectedSet.Has(recordedEmployee) {
					recordedNotSelected[recordedEmployee] = true
					toDeleteLifecycleFlzEmployees = append(toDeleteLifecycleFlzEmployees, recordedEmployee)
				}
			}
		}
	}

	ns := employer.GetNamespace()
	lifecycleFlz := GenerateLifecycleFinalizer(employer.GetName())
	err = r.ensureLifecycleFinalizer(ctx, ns, lifecycleFlz, toAddLifecycleFlzEmployees, toDeleteLifecycleFlzEmployees)
	if err != nil {
		return false, false, fmt.Errorf("ensureLifecycleFinalizer failed, err: %s", err.Error())
	}

	if needRecordEmployees {
		needUpdate := false
		if employer.GetAnnotations()[lifecycleFinalizerRecordedAnnoKey] == "" {
			if len(toAddLifecycleFlzEmployees) != 0 {
				needUpdate = true
			}
		} else {
			recordedEmployees := strings.Split(employer.GetAnnotations()[lifecycleFinalizerRecordedAnnoKey], ",")
			if !reflect.DeepEqual(recordedEmployees, toAddLifecycleFlzEmployees) {
				needUpdate = true
			}
		}
		if needUpdate {
			patch := client.MergeFrom(employer.DeepCopyObject().(client.Object))
			annos := employer.GetAnnotations()
			if annos == nil {
				annos = make(map[string]string)
			}
			annos[lifecycleFinalizerRecordedAnnoKey] = strings.Join(toAddLifecycleFlzEmployees, ",")
			employer.SetAnnotations(annos)
			err = r.Client.Patch(ctx, employer, patch)
			if err != nil {
				return false, false, fmt.Errorf("patch lifecycleFinalizerRecordedAnno failed, err: %s", err.Error())
			}
		}
	}

	isClean := len(toCudEmployees.ToCreate) == 0 && len(toCudEmployees.ToUpdate) == 0 && len(toCudEmployees.Unchanged) == 0 && len(failDelete) == 0
	cudFailedExist := len(failCreate) > 0 || len(failUpdate) > 0 || len(failDelete) > 0
	return isClean, cudFailedExist, nil
}

// ensureExpectFinalizer add expected finalizer to employee's available condition anno
func (r *Consist) ensureExpectedFinalizer(ctx context.Context, employer client.Object) (bool, error) {
	// employee is not pod or not follow PodOpsLifecycle
	watchOptions, watchOptionsImplemented := r.adapter.(ReconcileWatchOptions)
	lifecycleOptions, lifecycleOptionsImplemented := r.adapter.(ReconcileLifecycleOptions)
	if (lifecycleOptionsImplemented && !lifecycleOptions.FollowPodOpsLifeCycle()) || (watchOptionsImplemented && !isPod(watchOptions.NewEmployee())) {
		return true, nil
	}

	selectedEmployeeNames, err := r.adapter.GetSelectedEmployeeNames(ctx, employer)
	if err != nil {
		return false, fmt.Errorf("get selected employees' names failed, err: %s", err.Error())
	}

	addedExpectedFinalizerPodNames := strings.Split(employer.GetAnnotations()[expectedFinalizerAddedAnnoKey], ",")

	var toAdd, toDelete []PodExpectedFinalizerOps
	if !employer.GetDeletionTimestamp().IsZero() {
		toDeleteNames := sets.NewString(addedExpectedFinalizerPodNames...).Insert(selectedEmployeeNames...).List()
		for _, podName := range toDeleteNames {
			toDelete = append(toDelete, PodExpectedFinalizerOps{
				Name:    podName,
				Succeed: false,
			})
		}
		_ = r.patchPodExpectedFinalizer(ctx, employer, toAdd, toDelete)
		var notDeletedPodNames []string
		for _, deleteExpectedFinalizerOps := range toDelete {
			if !deleteExpectedFinalizerOps.Succeed {
				notDeletedPodNames = append(notDeletedPodNames, deleteExpectedFinalizerOps.Name)
			}
		}
		patch := client.MergeFrom(employer.DeepCopyObject().(client.Object))
		annos := employer.GetAnnotations()
		if annos == nil {
			annos = make(map[string]string)
		}
		if annos[expectedFinalizerAddedAnnoKey] == strings.Join(notDeletedPodNames, ",") {
			return len(notDeletedPodNames) == 0, nil
		}
		annos[expectedFinalizerAddedAnnoKey] = strings.Join(notDeletedPodNames, ",")
		employer.SetAnnotations(annos)
		return len(notDeletedPodNames) == 0, r.Client.Patch(ctx, employer, patch)
	}

	selectedSet := sets.NewString(selectedEmployeeNames...)
	for _, podName := range addedExpectedFinalizerPodNames {
		if !selectedSet.Has(podName) {
			toDelete = append(toDelete, PodExpectedFinalizerOps{
				Name:    podName,
				Succeed: false,
			})
		}
	}

	addedSet := sets.NewString(addedExpectedFinalizerPodNames...)
	for _, podName := range selectedEmployeeNames {
		if !addedSet.Has(podName) {
			toAdd = append(toAdd, PodExpectedFinalizerOps{
				Name:    podName,
				Succeed: false,
			})
		}
	}

	_ = r.patchPodExpectedFinalizer(ctx, employer, toAdd, toDelete)
	var succDeletedNames []string
	for _, deleteExpectFinalizerOps := range toDelete {
		if deleteExpectFinalizerOps.Succeed {
			succDeletedNames = append(succDeletedNames, deleteExpectFinalizerOps.Name)
		}
	}
	succDeletedNamesSet := sets.NewString(succDeletedNames...)
	var addedNames []string
	for _, added := range addedExpectedFinalizerPodNames {
		if !succDeletedNamesSet.Has(added) {
			addedNames = append(addedNames, added)
		}
	}
	for _, addExpectedFinalizerOps := range toAdd {
		if addExpectedFinalizerOps.Succeed {
			addedNames = append(addedNames, addExpectedFinalizerOps.Name)
		}
	}

	patch := client.MergeFrom(employer.DeepCopyObject().(client.Object))
	annos := employer.GetAnnotations()
	if annos == nil {
		annos = make(map[string]string)
	}
	if annos[expectedFinalizerAddedAnnoKey] == strings.Join(addedNames, ",") {
		return len(addedNames) == 0, nil
	}
	annos[expectedFinalizerAddedAnnoKey] = strings.Join(addedNames, ",")
	employer.SetAnnotations(annos)
	return len(addedNames) == 0, r.Client.Patch(ctx, employer, patch)
}

func (r *Consist) patchPodExpectedFinalizer(ctx context.Context, employer client.Object, toAdd, toDelete []PodExpectedFinalizerOps) error {
	expectedFlzKey := GenerateLifecycleFinalizerKey(employer)
	expectedFlz := GenerateLifecycleFinalizer(employer.GetName())

	errAdd := r.patchAddPodExpectedFinalizer(ctx, employer, toAdd, expectedFlzKey, expectedFlz)
	errDelete := r.patchDeletePodExpectedFinalizer(ctx, employer, toDelete, expectedFlzKey)

	return errors2.NewAggregate([]error{errAdd, errDelete})
}

func (r *Consist) patchAddPodExpectedFinalizer(ctx context.Context, employer client.Object, toAdd []PodExpectedFinalizerOps,
	expectedFlzKey, expectedFlz string) error {
	_, err := utils.SlowStartBatch(len(toAdd), 1, false, func(i int, _ error) error {
		podExpectedFinalizerOps := &toAdd[i]
		var pod corev1.Pod
		err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: employer.GetNamespace(),
			Name:      podExpectedFinalizerOps.Name,
		}, &pod)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		if !pod.GetDeletionTimestamp().IsZero() {
			return nil
		}

		patch := client.MergeFrom(pod.DeepCopy())

		var availableExpectedFlzs v1alpha1.PodAvailableConditions
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		if pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] == "" {
			availableExpectedFlzs.ExpectedFinalizers = map[string]string{expectedFlzKey: expectedFlz}
			annoAvailableExpectedFlzs, errMarshal := json.Marshal(availableExpectedFlzs)
			if errMarshal != nil {
				return errMarshal
			}
			pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] = string(annoAvailableExpectedFlzs)
			errPatch := r.Client.Patch(ctx, &pod, patch)
			if errPatch != nil {
				return errPatch
			}
		} else {
			errUnmarshal := json.Unmarshal([]byte(pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation]), &availableExpectedFlzs)
			if errUnmarshal != nil {
				return errUnmarshal
			}
			if availableExpectedFlzs.ExpectedFinalizers == nil {
				availableExpectedFlzs.ExpectedFinalizers = make(map[string]string)
			}
			if availableExpectedFlzs.ExpectedFinalizers[expectedFlzKey] != expectedFlz {
				availableExpectedFlzs.ExpectedFinalizers[expectedFlzKey] = expectedFlz
				annoAvailableExpectedFlzs, errMarshal := json.Marshal(availableExpectedFlzs)
				if errMarshal != nil {
					return errMarshal
				}
				pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] = string(annoAvailableExpectedFlzs)
				errPatch := r.Client.Patch(ctx, &pod, patch)
				if errPatch != nil {
					return errPatch
				}
			}
		}
		podExpectedFinalizerOps.Succeed = true
		return nil
	})

	return err
}

func (r *Consist) patchDeletePodExpectedFinalizer(ctx context.Context, employer client.Object, toDelete []PodExpectedFinalizerOps,
	expectedFlzKey string) error {
	_, err := utils.SlowStartBatch(len(toDelete), 1, false, func(i int, _ error) error {
		podExpectedFinalizerOps := &toDelete[i]
		var pod corev1.Pod
		err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: employer.GetNamespace(),
			Name:      podExpectedFinalizerOps.Name,
		}, &pod)
		if err != nil {
			if errors.IsNotFound(err) {
				podExpectedFinalizerOps.Succeed = true
				return nil
			}
			return err
		}

		patch := client.MergeFrom(pod.DeepCopy())

		var availableExpectedFlzs v1alpha1.PodAvailableConditions
		if pod.Annotations == nil || pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] == "" {
			podExpectedFinalizerOps.Succeed = true
			return nil
		}

		errUnmarshal := json.Unmarshal([]byte(pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation]), &availableExpectedFlzs)
		if errUnmarshal != nil {
			return errUnmarshal
		}
		if _, exist := availableExpectedFlzs.ExpectedFinalizers[expectedFlzKey]; exist {
			delete(availableExpectedFlzs.ExpectedFinalizers, expectedFlzKey)
			annoAvailableExpectedFlzs, errMarshal := json.Marshal(availableExpectedFlzs)
			if errMarshal != nil {
				return errMarshal
			}
			pod.Annotations[v1alpha1.PodAvailableConditionsAnnotation] = string(annoAvailableExpectedFlzs)
			errPatch := r.Client.Patch(ctx, &pod, patch)
			if errPatch != nil {
				return errPatch
			}
		}
		podExpectedFinalizerOps.Succeed = true
		return nil
	})

	return err
}

func (r *Consist) cleanEmployerCleanFinalizer(ctx context.Context, employer client.Object) error {
	var employerLatest client.Object
	if watchOptions, ok := r.adapter.(ReconcileWatchOptions); ok {
		employerLatest = watchOptions.NewEmployer()
	} else {
		employerLatest = &corev1.Service{}
	}

	err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: employer.GetNamespace(),
		Name:      employer.GetName(),
	}, employerLatest)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	alreadyDeleted := true
	var finalizers []string
	cleanFlz := cleanFinalizerPrefix + employer.GetName()
	for _, flz := range employer.GetFinalizers() {
		if flz == cleanFlz {
			alreadyDeleted = false
			continue
		}
		finalizers = append(finalizers, flz)
	}
	if alreadyDeleted {
		return nil
	}
	employerLatest.SetFinalizers(finalizers)
	return r.Client.Update(ctx, employerLatest)
}

func (r *Consist) ensureLifecycleFinalizer(ctx context.Context, ns, lifecycleFlz string, toAdd, toDelete []string) error {
	watchOptions, watchOptionsImplemented := r.adapter.(ReconcileWatchOptions)

	_, err := utils.SlowStartBatch(len(toAdd), 1, false, func(i int, _ error) error {
		employeeName := toAdd[i]
		var employee client.Object
		if watchOptionsImplemented {
			employee = watchOptions.NewEmployee()
		} else {
			employee = &corev1.Pod{}
		}
		err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: ns,
			Name:      employeeName,
		}, employee)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		alreadyAdd := false
		for _, flz := range employee.GetFinalizers() {
			if flz == lifecycleFlz {
				alreadyAdd = true
				break
			}
		}
		if alreadyAdd {
			return nil
		}
		employee.SetFinalizers(append(employee.GetFinalizers(), lifecycleFlz))
		return r.Client.Update(ctx, employee)
	})
	if err != nil {
		return err
	}

	_, err = utils.SlowStartBatch(len(toDelete), 1, false, func(i int, _ error) error {
		employeeName := toDelete[i]
		var employee client.Object
		if watchOptionsImplemented {
			employee = watchOptions.NewEmployee()
		} else {
			employee = &corev1.Pod{}
		}
		err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: ns,
			Name:      employeeName,
		}, employee)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
		alreadyDeleted := true
		var finalizers []string
		for _, flz := range employee.GetFinalizers() {
			if flz == lifecycleFlz {
				alreadyDeleted = false
				continue
			}
			finalizers = append(finalizers, flz)
		}
		if alreadyDeleted {
			return nil
		}
		employee.SetFinalizers(finalizers)
		return r.Client.Update(ctx, employee)
	})
	return err
}

func (r *Consist) getToAddDeleteLifecycleFlzEmployees(succCreate, succDelete, succUpdate, unchanged []IEmployee) ([]string, []string) {
	toAddLifecycleFlz := make([]string, len(succCreate)+len(succUpdate)+len(unchanged))
	toDeleteLifecycleFlz := make([]string, len(succDelete)+len(succUpdate)+len(unchanged))
	toAddIdx, toDeleteIdx := 0, 0

	watchOptions, watchOptionsImplemented := r.adapter.(ReconcileWatchOptions)

	lifecycleOptions, lifecycleOptionsImplemented := r.adapter.(ReconcileLifecycleOptions)
	if (lifecycleOptionsImplemented && !lifecycleOptions.FollowPodOpsLifeCycle()) || (watchOptionsImplemented && !isPod(watchOptions.NewEmployee())) {
		return toAddLifecycleFlz[:toAddIdx], toDeleteLifecycleFlz[:toDeleteIdx]
	}

	for _, employee := range succCreate {
		toAddLifecycleFlz[toAddIdx] = employee.GetEmployeeName()
		toAddIdx++
	}

	for _, employee := range succUpdate {
		podEmployeeStatus, ok := employee.GetEmployeeStatuses().(PodEmployeeStatuses)
		if !ok {
			continue
		}
		if podEmployeeStatus.LifecycleReady {
			toAddLifecycleFlz[toAddIdx] = employee.GetEmployeeName()
			toAddIdx++
			continue
		}
		toDeleteLifecycleFlz[toDeleteIdx] = employee.GetEmployeeName()
		toDeleteIdx++
	}

	for _, employee := range succDelete {
		toDeleteLifecycleFlz[toDeleteIdx] = employee.GetEmployeeName()
		toDeleteIdx++
	}

	for _, employee := range unchanged {
		podEmployeeStatus, ok := employee.GetEmployeeStatuses().(PodEmployeeStatuses)
		if !ok {
			continue
		}
		if podEmployeeStatus.LifecycleReady {
			toAddLifecycleFlz[toAddIdx] = employee.GetEmployeeName()
			toAddIdx++
			continue
		}
		toDeleteLifecycleFlz[toDeleteIdx] = employee.GetEmployeeName()
		toDeleteIdx++
	}

	return toAddLifecycleFlz[:toAddIdx], toDeleteLifecycleFlz[:toDeleteIdx]
}

func (r *Consist) ensureEmployerCleanFlz(ctx context.Context, employer client.Object) (bool, error) {
	if !employer.GetDeletionTimestamp().IsZero() {
		return false, nil
	}
	for _, flz := range employer.GetFinalizers() {
		if flz == cleanFinalizerPrefix+employer.GetName() {
			return false, nil
		}
	}
	employer.SetFinalizers(append(employer.GetFinalizers(), cleanFinalizerPrefix+employer.GetName()))
	return true, r.Client.Update(ctx, employer)
}
