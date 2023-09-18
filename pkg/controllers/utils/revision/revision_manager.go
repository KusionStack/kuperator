/*
Copyright 2017 The Kubernetes Authors.
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

package revision

import (
	"bytes"
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"

	apps "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	refmanagerutil "kusionstack.io/operating/pkg/controllers/utils/refmanager"
)

const ControllerRevisionHashLabel = "controller.kubernetes.io/hash"

type OwnerAdapter interface {
	GetSelector(obj metav1.Object) *metav1.LabelSelector
	GetCollisionCount(obj metav1.Object) *int32
	GetHistoryLimit(obj metav1.Object) int32
	GetPatch(obj metav1.Object) ([]byte, error)
	GetSelectorLabels(obj metav1.Object) map[string]string
	GetCurrentRevision(obj metav1.Object) string
	IsInUsed(obj metav1.Object, controllerRevision string) bool
}

func NewRevisionManager(client client.Client, scheme *runtime.Scheme, ownerGetter OwnerAdapter) *RevisionManager {
	return &RevisionManager{
		Client:      client,
		scheme:      scheme,
		ownerGetter: ownerGetter,
	}
}

type RevisionManager struct {
	client.Client

	scheme      *runtime.Scheme
	ownerGetter OwnerAdapter
}

// controlledHistories returns all ControllerRevisions controlled by the given DaemonSet.
// This also reconciles ControllerRef by adopting/orphaning.
// Note that returned histories are pointers to objects in the cache.
// If you want to modify one, you need to deep-copy it first.
func controlledHistories(c client.Client, owner client.Object, labelSelector *metav1.LabelSelector, scheme *runtime.Scheme) ([]*apps.ControllerRevision, error) {
	// List all histories to include those that don't match the selector anymore
	// but have a ControllerRef pointing to the controller.
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}
	histories := &apps.ControllerRevisionList{}
	if err := c.List(context.TODO(), histories, &client.ListOptions{Namespace: owner.GetNamespace(), LabelSelector: selector}); err != nil {
		return nil, err
	}

	// Use ControllerRefManager to adopt/orphan as needed.
	cm, err := refmanagerutil.NewRefManager(c, labelSelector, owner, scheme)
	if err != nil {
		return nil, err
	}
	mts := make([]client.Object, len(histories.Items))
	for i, pod := range histories.Items {
		mts[i] = pod.DeepCopy()
	}
	claims, err := cm.ClaimOwned(mts)
	if err != nil {
		return nil, err
	}

	claimHistories := make([]*apps.ControllerRevision, len(claims))
	for i, mt := range claims {
		claimHistories[i] = mt.(*apps.ControllerRevision)
	}

	return claimHistories, nil
}

// ConstructRevisions returns the current and update ControllerRevisions for set. It also
// returns a collision count that records the number of name collisions set saw when creating
// new ControllerRevisions. This count is incremented on every name collision and is used in
// building the ControllerRevision names for name collision avoidance. This method may create
// a new revision, or modify the Revision of an existing revision if an update to set is detected.
// This method expects that revisions is sorted when supplied.
func (rm *RevisionManager) ConstructRevisions(set client.Object, dryRun bool) (*apps.ControllerRevision, *apps.ControllerRevision, []*apps.ControllerRevision, *int32, bool, error) {
	var currentRevision, updateRevision *apps.ControllerRevision
	revisions, err := controlledHistories(rm.Client, set, rm.ownerGetter.GetSelector(set), rm.scheme)
	if err != nil {
		return currentRevision, updateRevision, nil, rm.ownerGetter.GetCollisionCount(set), false, err
	}

	SortControllerRevisions(revisions)
	if cleanedRevision, err := rm.cleanExpiredRevision(set, &revisions); err != nil {
		return currentRevision, updateRevision, nil, rm.ownerGetter.GetCollisionCount(set), false, err
	} else {
		revisions = *cleanedRevision
	}

	collisionCount := new(int32)
	if rm.ownerGetter.GetCollisionCount(set) != nil {
		collisionCount = rm.ownerGetter.GetCollisionCount(set)
	}
	// create a new revision from the current set
	updateRevision, err = rm.newRevision(set, nextRevision(revisions), collisionCount)
	if err != nil {
		return nil, nil, nil, collisionCount, false, err
	}

	// find any equivalent revisions
	equalRevisions := FindEqualRevisions(revisions, updateRevision)
	equalCount := len(equalRevisions)
	revisionCount := len(revisions)

	createNewRevision := false
	if equalCount > 0 && EqualRevision(revisions[revisionCount-1], equalRevisions[equalCount-1]) {
		// if the equivalent revision is immediately prior the update revision has not changed
		updateRevision = revisions[revisionCount-1]
	} else if equalCount > 0 {
		// if the equivalent revision is not immediately prior we will roll back by incrementing the
		// Revision of the equivalent revision
		equalRevisions[equalCount-1].Revision = updateRevision.Revision
		rvc := equalRevisions[equalCount-1]
		err = rm.Update(context.TODO(), rvc)
		if err != nil {
			return nil, nil, nil, collisionCount, false, err
		}
		equalRevisions[equalCount-1] = rvc
		updateRevision = equalRevisions[equalCount-1]
	} else {
		if !dryRun {
			//if there is no equivalent revision we create a new one
			updateRevision, err = rm.createControllerRevision(context.TODO(), set, updateRevision, collisionCount)
			if err != nil {
				return nil, nil, nil, collisionCount, false, err
			}
		}

		createNewRevision = true
	}

	// attempt to find the revision that corresponds to the current revision
	for i := range revisions {
		if revisions[i].Name == rm.ownerGetter.GetCurrentRevision(set) {
			currentRevision = revisions[i]
		}
	}

	// if the current revision is nil we initialize the history by setting it to the update revision
	if currentRevision == nil {
		currentRevision = updateRevision
	}

	return currentRevision, updateRevision, revisions, collisionCount, createNewRevision, nil
}

func (rm *RevisionManager) cleanExpiredRevision(cd metav1.Object, sortedRevisions *[]*apps.ControllerRevision) (*[]*apps.ControllerRevision, error) {
	limit := int(rm.ownerGetter.GetHistoryLimit(cd))
	if limit <= 0 {
		limit = 10
	}

	// reserve 2 extra unused revisions for diagnose
	exceedNum := len(*sortedRevisions) - limit - 2
	if exceedNum <= 0 {
		return sortedRevisions, nil
	}

	for _, revision := range *sortedRevisions {
		if exceedNum == 0 {
			break
		}

		if rm.ownerGetter.IsInUsed(cd, revision.Name) {
			continue
		}

		if err := rm.Delete(context.TODO(), revision); err != nil {
			return sortedRevisions, err
		}

		exceedNum--
	}
	cleanedRevisions := (*sortedRevisions)[exceedNum:]

	return &cleanedRevisions, nil
}

func (rm *RevisionManager) createControllerRevision(ctx context.Context, parent metav1.Object, revision *apps.ControllerRevision, collisionCount *int32) (*apps.ControllerRevision, error) {
	if collisionCount == nil {
		return nil, fmt.Errorf("collisionCount should not be nil")
	}

	// Clone the input
	clone := revision.DeepCopy()

	var err error
	// Continue to attempt to create the revision updating the name with a new hash on each iteration
	for {
		hash := hashControllerRevision(revision, collisionCount)
		// Update the revisions name
		clone.Name = controllerRevisionName(parent.GetName(), hash)
		err = rm.Create(ctx, clone)
		if errors.IsAlreadyExists(err) {
			exists := &apps.ControllerRevision{}
			err := rm.Get(ctx, types.NamespacedName{Namespace: clone.Namespace, Name: clone.Name}, exists)
			if err != nil {
				return nil, err
			}
			if bytes.Equal(exists.Data.Raw, clone.Data.Raw) {
				return exists, nil
			}
			*collisionCount++
			continue
		}
		return clone, err
	}
}

// controllerRevisionName returns the Name for a ControllerRevision in the form prefix-hash. If the length
// of prefix is greater than 223 bytes, it is truncated to allow for a name that is no larger than 253 bytes.
func controllerRevisionName(prefix string, hash string) string {
	if len(prefix) > 223 {
		prefix = prefix[:223]
	}

	return fmt.Sprintf("%s-%s", prefix, hash)
}

// hashControllerRevision hashes the contents of revision's Data using FNV hashing. If probe is not nil, the byte value
// of probe is added written to the hash as well. The returned hash will be a safe encoded string to avoid bad words.
func hashControllerRevision(revision *apps.ControllerRevision, probe *int32) string {
	hf := fnv.New32()
	if len(revision.Data.Raw) > 0 {
		hf.Write(revision.Data.Raw)
	}
	if revision.Data.Object != nil {
		DeepHashObject(hf, revision.Data.Object)
	}
	if probe != nil {
		hf.Write([]byte(strconv.FormatInt(int64(*probe), 10)))
	}
	return rand.SafeEncodeString(fmt.Sprint(hf.Sum32()))
}

// newRevision creates a new ControllerRevision containing a patch that reapplies the target state of set.
// The Revision of the returned ControllerRevision is set to revision. If the returned error is nil, the returned
// ControllerRevision is valid. StatefulSet revisions are stored as patches that re-apply the current state of set
// to a new StatefulSet using a strategic merge patch to replace the saved state of the new StatefulSet.
func (rm *RevisionManager) newRevision(set metav1.Object, revision int64, collisionCount *int32) (*apps.ControllerRevision, error) {
	patch, err := rm.ownerGetter.GetPatch(set)
	if err != nil {
		return nil, err
	}

	runtimeObj, ok := set.(runtime.Object)
	if !ok {
		return nil, fmt.Errorf("revision owner %s/%s does not implement runtime Object interface", set.GetNamespace(), set.GetName())
	}
	gvk, err := apiutil.GVKForObject(runtimeObj, rm.scheme)
	if err != nil {
		return nil, err
	}

	revisionLabels := rm.ownerGetter.GetSelectorLabels(set)
	if revisionLabels == nil {
		revisionLabels = map[string]string{}
	}

	if selector := rm.ownerGetter.GetSelector(set); selector != nil {
		for k, v := range selector.MatchLabels {
			revisionLabels[k] = v
		}
	}

	cr, err := newControllerRevision(set,
		gvk,
		revisionLabels,
		runtime.RawExtension{Raw: patch},
		revision,
		collisionCount)
	if err != nil {
		return nil, err
	}

	cr.Namespace = set.GetNamespace()
	if cr.ObjectMeta.Annotations == nil {
		cr.ObjectMeta.Annotations = make(map[string]string)
	}
	for key, value := range set.GetAnnotations() {
		cr.ObjectMeta.Annotations[key] = value
	}

	if cr.ObjectMeta.Labels == nil {
		cr.ObjectMeta.Labels = make(map[string]string)
	}

	return cr, nil
}

// nextRevision finds the next valid revision number based on revisions. If the length of revisions
// is 0 this is 1. Otherwise, it is 1 greater than the largest revision's Revision. This method
// assumes that revisions has been sorted by Revision.
func nextRevision(revisions []*apps.ControllerRevision) int64 {
	count := len(revisions)
	if count <= 0 {
		return 1
	}
	return revisions[count-1].Revision + 1
}

// SortControllerRevisions sorts revisions by their Revision.
func SortControllerRevisions(revisions []*apps.ControllerRevision) {
	sort.Sort(byRevision(revisions))
}

// byRevision implements sort.Interface to allow ControllerRevisions to be sorted by Revision.
type byRevision []*apps.ControllerRevision

func (br byRevision) Len() int {
	return len(br)
}

func (br byRevision) Less(i, j int) bool {
	return br[i].Revision < br[j].Revision
}

func (br byRevision) Swap(i, j int) {
	br[i], br[j] = br[j], br[i]
}

// EqualRevision returns true if lhs and rhs are either both nil, or both have same labels and annotations, or bath point
// to non-nil ControllerRevisions that contain semantically equivalent data. Otherwise this method returns false.
func EqualRevision(lhs *apps.ControllerRevision, rhs *apps.ControllerRevision) bool {
	var lhsHash, rhsHash *uint32
	if lhs == nil || rhs == nil {
		return lhs == rhs
	}

	if hs, found := lhs.Labels[ControllerRevisionHashLabel]; found {
		hash, err := strconv.ParseInt(hs, 10, 32)
		if err == nil {
			lhsHash = new(uint32)
			*lhsHash = uint32(hash)
		}
	}
	if hs, found := rhs.Labels[ControllerRevisionHashLabel]; found {
		hash, err := strconv.ParseInt(hs, 10, 32)
		if err == nil {
			rhsHash = new(uint32)
			*rhsHash = uint32(hash)
		}
	}
	if lhsHash != nil && rhsHash != nil && *lhsHash != *rhsHash {
		return false
	}
	return bytes.Equal(lhs.Data.Raw, rhs.Data.Raw) && apiequality.Semantic.DeepEqual(lhs.Data.Object, rhs.Data.Object)
}

// FindEqualRevisions returns all ControllerRevisions in revisions that are equal to needle using EqualRevision as the
// equality test. The returned slice preserves the order of revisions.
func FindEqualRevisions(revisions []*apps.ControllerRevision, needle *apps.ControllerRevision) []*apps.ControllerRevision {
	var eq []*apps.ControllerRevision
	for i := range revisions {
		if EqualRevision(revisions[i], needle) {
			eq = append(eq, revisions[i])
		}
	}
	return eq
}

// newControllerRevision returns a ControllerRevision with a ControllerRef pointing to parent and indicating that
// parent is of parentKind. The ControllerRevision has labels matching template labels, contains Data equal to data, and
// has a Revision equal to revision. The collisionCount is used when creating the name of the ControllerRevision
// so the name is likely unique. If the returned error is nil, the returned ControllerRevision is valid. If the
// returned error is not nil, the returned ControllerRevision is invalid for use.
func newControllerRevision(parent metav1.Object,
	parentKind schema.GroupVersionKind,
	templateLabels map[string]string,
	data runtime.RawExtension,
	revision int64,
	collisionCount *int32) (*apps.ControllerRevision, error) {
	labelMap := make(map[string]string)
	for k, v := range templateLabels {
		labelMap[k] = v
	}
	blockOwnerDeletion := true
	isController := true
	cr := &apps.ControllerRevision{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labelMap,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         parentKind.GroupVersion().String(),
					Kind:               parentKind.Kind,
					Name:               parent.GetName(),
					UID:                parent.GetUID(),
					BlockOwnerDeletion: &blockOwnerDeletion,
					Controller:         &isController,
				},
			},
		},
		Data:     data,
		Revision: revision,
	}
	hash := hashControllerRevision(cr, collisionCount)
	cr.Name = controllerRevisionName(parent.GetName(), hash)
	cr.Labels[ControllerRevisionHashLabel] = hash
	return cr, nil
}
