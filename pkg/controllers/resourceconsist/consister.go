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
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"kusionstack.io/kafed/pkg/controllers/utils"
)

func (r *Consist) syncEmployer(ctx context.Context, employer client.Object, expectEmployerStatus, currentEmployerStatus []IEmployerStatus) (bool, error) {
	toCudEmployer, err := r.diffEmployer(expectEmployerStatus, currentEmployerStatus)
	if err != nil {
		return false, fmt.Errorf("diff employer failed, err: %s", err.Error())
	}
	succCreate, _, err := r.adapter.CreateEmployer(employer, toCudEmployer.ToCreate)
	if err != nil {
		return false, fmt.Errorf("syncCreate failed, err: %s", err.Error())
	}
	succUpdate, _, err := r.adapter.UpdateEmployer(employer, toCudEmployer.ToUpdate)
	if err != nil {
		return false, fmt.Errorf("syncUpdate failed, err: %s", err.Error())
	}
	succDelete, _, err := r.adapter.DeleteEmployer(employer, toCudEmployer.ToDelete)
	if err != nil {
		return false, fmt.Errorf("syncDelete failed, err: %s", err.Error())
	}

	err = r.adapter.RecordEmployer(succCreate, succUpdate, succDelete)
	if err != nil {
		return false, fmt.Errorf("record employer failed, err: %s", err.Error())
	}

	isClean := len(toCudEmployer.Unchanged) == 0 && len(toCudEmployer.ToCreate) == 0 && len(toCudEmployer.ToUpdate) == 0 && len(toCudEmployer.ToDelete) == 0
	return isClean, nil
}

func (r *Consist) diffEmployer(expectEmployer, currentEmployer []IEmployerStatus) (ToCUDEmployer, error) {
	expectEmployerMap := make(map[string]IEmployerStatus)
	currentEmployerMap := make(map[string]IEmployerStatus)

	for _, expect := range expectEmployer {
		expectEmployerMap[expect.GetEmployerId()] = expect
	}
	for _, current := range currentEmployer {
		currentEmployerMap[current.GetEmployerId()] = current
	}

	toCreate := make([]IEmployerStatus, len(expectEmployer))
	toUpdate := make([]IEmployerStatus, len(currentEmployer))
	toDelete := make([]IEmployerStatus, len(currentEmployer))
	unchanged := make([]IEmployerStatus, len(currentEmployer))
	toCreateIdx, toUpdateIdx, toDeleteIdx, unchangedIdx := 0, 0, 0, 0

	for expectId, expect := range expectEmployerMap {
		current, exist := currentEmployerMap[expectId]
		if !exist {
			toCreate[toCreateIdx] = expect
			toCreateIdx++
			continue
		}
		equal, err := expect.EmployerEqual(current.GetEmployerStatuses())
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
	r.logger.Infof("toCreate: %v, toUpdate: %v, toDelete: %v, unchanged: %v",
		toCreate[:toCreateIdx], toUpdate[:toUpdateIdx], toDelete[:toDeleteIdx], unchanged[:unchangedIdx])

	return ToCUDEmployer{
		ToCreate:  toCreate[:toCreateIdx],
		ToUpdate:  toUpdate[:toUpdateIdx],
		ToDelete:  toDelete[:toDeleteIdx],
		Unchanged: unchanged[:unchangedIdx],
	}, nil
}

func (r *Consist) diffEmployees(expectEmployees, currentEmployees []IEmployeeStatus) (ToCUDEmployees, error) {
	expectEmployeesMap := make(map[string]IEmployeeStatus)
	currentEmployeesMap := make(map[string]IEmployeeStatus)

	for _, expect := range expectEmployees {
		expectEmployeesMap[expect.GetEmployeeId()] = expect
	}
	for _, current := range currentEmployees {
		currentEmployeesMap[current.GetEmployeeId()] = current
	}

	toCreate := make([]IEmployeeStatus, len(expectEmployees))
	toUpdate := make([]IEmployeeStatus, len(currentEmployees))
	toDelete := make([]IEmployeeStatus, len(currentEmployees))
	unchanged := make([]IEmployeeStatus, len(currentEmployees))
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
	r.logger.Infof("toCreate: %v, toUpdate: %v, toDelete: %v, unchanged: %v",
		toCreate[:toCreateIdx], toUpdate[:toUpdateIdx], toDelete[toDeleteIdx], unchanged[:unchangedIdx])

	return ToCUDEmployees{
		ToCreate:  toCreate[:toCreateIdx],
		ToUpdate:  toUpdate[:toUpdateIdx],
		ToDelete:  toDelete[:toDeleteIdx],
		Unchanged: unchanged[:unchangedIdx],
	}, nil
}

func (r *Consist) syncEmployees(ctx context.Context, employer client.Object, expectEmployees, currentEmployees []IEmployeeStatus) (bool, error) {
	// get expect/current employees diffEmployees
	toCudEmployees, err := r.diffEmployees(expectEmployees, currentEmployees)
	if err != nil {
		return false, err
	}

	succCreate, failCreate, err := r.adapter.CreateEmployees(employer, toCudEmployees.ToCreate)
	if err != nil {
		return false, fmt.Errorf("syncCreate failed, err: %s", err.Error())
	}
	succUpdate, _, err := r.adapter.UpdateEmployees(employer, toCudEmployees.ToUpdate)
	if err != nil {
		return false, fmt.Errorf("syncUpdate failed, err: %s", err.Error())
	}
	succDelete, failDelete, err := r.adapter.DeleteEmployees(employer, toCudEmployees.ToDelete)
	if err != nil {
		return false, fmt.Errorf("syncDelete failed, err: %s", err.Error())
	}

	toAddLifecycleFlzEmployees, toDeleteLifecycleFlzEmployees := r.getToAddDeleteLifecycleFlzEmployees(
		succCreate, failCreate, succDelete, failDelete, succUpdate, toCudEmployees.Unchanged)

	ns := employer.GetNamespace()
	lifecycleFlz := lifecycleFinalizerPrefix + employer.GetName()
	err = r.ensureLifecycleFinalizer(ctx, ns, lifecycleFlz, toAddLifecycleFlzEmployees, toDeleteLifecycleFlzEmployees)
	if err != nil {
		return false, fmt.Errorf("ensureLifecycleFinalizer failed, err: %s", err.Error())
	}

	isClean := len(toCudEmployees.ToCreate) == 0 && len(toCudEmployees.ToUpdate) == 0 && len(toCudEmployees.Unchanged) == 0 && len(failDelete) == 0
	return isClean, nil
}

func (r *Consist) cleanEmployerCleanFinalizer(ctx context.Context, employer client.Object) error {
	employerLatest := r.adapter.EmployerResource()
	err := r.Get(ctx, types.NamespacedName{
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
	return r.Update(ctx, employerLatest)
}

func (r *Consist) ensureLifecycleFinalizer(ctx context.Context, ns, lifecycleFlz string, toAdd, toDelete []string) error {
	_, err := utils.SlowStartBatch(len(toAdd), 1, false, func(i int, _ error) error {
		employeeName := toAdd[i]
		employee := r.adapter.EmployeeResource()
		err := r.Get(ctx, types.NamespacedName{
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
		return r.Update(ctx, employee)
	})
	if err != nil {
		return err
	}

	_, err = utils.SlowStartBatch(len(toDelete), 1, false, func(i int, _ error) error {
		employeeName := toDelete[i]
		employee := r.adapter.EmployeeResource()
		err := r.Get(ctx, types.NamespacedName{
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
		return r.Update(ctx, employee)
	})
	return err
}

func (r *Consist) getToAddDeleteLifecycleFlzEmployees(succCreate, failCreate, succDelete, failDelete, succUpdate, unchanged []IEmployeeStatus) ([]string, []string) {
	toAddLifecycleFlz := make([]string, len(succCreate)+len(succUpdate)+len(unchanged))
	toDeleteLifecycleFlz := make([]string, len(succDelete)+len(succUpdate)+len(unchanged))
	toAddIdx, toDeleteIdx := 0, 0

	if !isPod(r.adapter.EmployeeResource()) || r.adapter.NotFollowPodOpsLifeCycle() {
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

func (r *Consist) ensureEmployerCleanFlzFirstAdd(ctx context.Context, employer client.Object) error {
	if !employer.GetDeletionTimestamp().IsZero() {
		return nil
	}
	alreadyAdd := false
	for _, flz := range employer.GetFinalizers() {
		if flz == cleanFinalizerPrefix+employer.GetName() {
			alreadyAdd = true
			break
		}
	}
	if alreadyAdd {
		return nil
	}
	employer.SetFinalizers(append(employer.GetFinalizers(), cleanFinalizerPrefix+employer.GetName()))
	r.Update(ctx, employer)
	return fmt.Errorf("employer clean finalizer first added, requeue")
}
