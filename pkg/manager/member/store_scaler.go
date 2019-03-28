// Copyright 2018 deepfabric, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package member

import (
	"fmt"
	"strconv"
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	"github.com/golang/glog"
	apps "k8s.io/api/apps/v1beta1"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type storeScaler struct {
	generalScaler
	podLister corelisters.PodLister
}

// NewStoreScaler returns a store Scaler
func NewStoreScaler(pdControl controller.PDControlInterface,
	pvcLister corelisters.PersistentVolumeClaimLister,
	pvcControl controller.PVCControlInterface,
	podLister corelisters.PodLister) Scaler {
	return &storeScaler{generalScaler{pdControl, pvcLister, pvcControl}, podLister}
}

func (csd *storeScaler) ScaleOut(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	if cc.StoreUpgrading() {
		resetReplicas(newSet, oldSet)
		return nil
	}

	_, err := csd.deleteDeferDeletingPVC(cc, oldSet.GetName(), v1alpha1.StoreMemberType, *oldSet.Spec.Replicas)
	if err != nil {
		resetReplicas(newSet, oldSet)
		return err
	}

	increaseReplicas(newSet, oldSet)
	return nil
}

func (csd *storeScaler) ScaleIn(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	// we can only remove one member at a time when scale down
	ordinal := *oldSet.Spec.Replicas - 1
	setName := oldSet.GetName()

	// store can not scale in when it is upgrading
	if cc.StoreUpgrading() {
		resetReplicas(newSet, oldSet)
		glog.Infof("the CellCluster: [%s/%s]'s store is upgrading,can not scale in until upgrade have completed",
			ns, ccName)
		return nil
	}

	// We need remove member from cluster before reducing statefulset replicas
	podName := ordinalPodName(v1alpha1.StoreMemberType, ccName, ordinal)
	pod, err := csd.podLister.Pods(ns).Get(podName)
	if err != nil {
		resetReplicas(newSet, oldSet)
		return err
	}
	for _, store := range cc.Status.Store.Stores {
		if store.PodName == podName {
			state := store.State
			id, err := strconv.ParseUint(store.ID, 10, 64)
			if err != nil {
				resetReplicas(newSet, oldSet)
				return err
			}
			if state != v1alpha1.StoreStateOffline {
				if err := csd.pdControl.GetPDClient(cc).DeleteStore(id); err != nil {
					resetReplicas(newSet, oldSet)
					return err
				}
			}
			resetReplicas(newSet, oldSet)
			return controller.RequeueErrorf("Store %s/%s store %d  still in cluster, state: %s", ns, podName, id, state)
		}
	}
	for id, store := range cc.Status.Store.TombstoneStores {
		if store.PodName == podName && pod.Labels[label.StoreIDLabelKey] == id {
			id, err := strconv.ParseUint(store.ID, 10, 64)
			if err != nil {
				resetReplicas(newSet, oldSet)
				return err
			}

			// TODO: double check if store is really not in Up/Offline/Down state
			glog.Infof("Store %s/%s store %d becomes tombstone", ns, podName, id)

			pvcName := ordinalPVCName(v1alpha1.StoreMemberType, setName, ordinal)
			pvc, err := csd.pvcLister.PersistentVolumeClaims(ns).Get(pvcName)
			if err != nil {
				resetReplicas(newSet, oldSet)
				return err
			}
			if pvc.Annotations == nil {
				pvc.Annotations = map[string]string{}
			}
			pvc.Annotations[label.AnnPVCDeferDeleting] = time.Now().Format(time.RFC3339)
			_, err = csd.pvcControl.UpdatePVC(cc, pvc)
			if err != nil {
				resetReplicas(newSet, oldSet)
				return err
			}

			decreaseReplicas(newSet, oldSet)
			return nil
		}
	}

	// store not found in CellCluster status,
	// this can happen when Store joins cluster but we haven't synced its status
	// so return error to wait another round for safety
	resetReplicas(newSet, oldSet)
	return fmt.Errorf("Store %s/%s not found in cluster", ns, podName)
}

type fakeStoreScaler struct{}

// NewFakeStoreScaler returns a fake store Scaler
func NewFakeStoreScaler() Scaler {
	return &fakeStoreScaler{}
}

func (fsd *fakeStoreScaler) ScaleOut(_ *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	increaseReplicas(newSet, oldSet)
	return nil
}

func (fsd *fakeStoreScaler) ScaleIn(_ *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	decreaseReplicas(newSet, oldSet)
	return nil
}
