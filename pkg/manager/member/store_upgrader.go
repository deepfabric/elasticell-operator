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
	"github.com/golang/glog"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
)

const (
	// EvictLeaderBeginTime is the key of evict Leader begin time
	EvictLeaderBeginTime = "evictLeaderBeginTime"
	// EvictLeaderTimeout is the timeout limit of evict leader
	EvictLeaderTimeout = 3 * time.Minute
)

type storeUpgrader struct {
	pdControl  controller.PDControlInterface
	podControl controller.PodControlInterface
	podLister  corelisters.PodLister
}

// NewStoreUpgrader returns a store Upgrader
func NewStoreUpgrader(pdControl controller.PDControlInterface,
	podControl controller.PodControlInterface,
	podLister corelisters.PodLister) Upgrader {
	return &storeUpgrader{
		pdControl:  pdControl,
		podControl: podControl,
		podLister:  podLister,
	}
}

func (csu *storeUpgrader) Upgrade(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	if cc.Status.PD.Phase == v1alpha1.UpgradePhase {
		_, podSpec, err := GetLastAppliedConfig(oldSet)
		if err != nil {
			return err
		}
		newSet.Spec.Template.Spec = *podSpec
		return nil
	}

	if !cc.Status.Store.Synced {
		return fmt.Errorf("Cellcluster: [%s/%s]'s store status sync failed, can not to be upgraded", ns, ccName)
	}

	cc.Status.Store.Phase = v1alpha1.UpgradePhase
	if !templateEqual(newSet.Spec.Template, oldSet.Spec.Template) {
		return nil
	}

	setUpgradePartition(newSet, *oldSet.Spec.UpdateStrategy.RollingUpdate.Partition)
	for i := cc.Status.Store.StatefulSet.Replicas - 1; i >= 0; i-- {
		store := csu.getStoreByOrdinal(cc, i)
		if store == nil {
			continue
		}
		podName := storePodName(ccName, i)
		pod, err := csu.podLister.Pods(ns).Get(podName)
		if err != nil {
			return err
		}
		revision, exist := pod.Labels[apps.ControllerRevisionHashLabelKey]
		if !exist {
			return controller.RequeueErrorf("cellcluster: [%s/%s]'s store pod: [%s] has no label: %s", ns, ccName, podName, apps.ControllerRevisionHashLabelKey)
		}

		if revision == cc.Status.Store.StatefulSet.UpdateRevision {

			if pod.Status.Phase != corev1.PodRunning {
				return controller.RequeueErrorf("cellcluster: [%s/%s]'s upgraded store pod: [%s] is not running", ns, ccName, podName)
			}
			if store.State != v1alpha1.StoreStateUp {
				return controller.RequeueErrorf("cellcluster: [%s/%s]'s upgraded store pod: [%s] is not all ready", ns, ccName, podName)
			}
			err := csu.endEvictLeader(cc, i)
			if err != nil {
				return err
			}
			continue
		}

		return csu.upgradeStorePod(cc, i, newSet)
	}

	return nil
}

func (csu *storeUpgrader) upgradeStorePod(cc *v1alpha1.CellCluster, ordinal int32, newSet *apps.StatefulSet) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	upgradePodName := storePodName(ccName, ordinal)
	upgradePod, err := csu.podLister.Pods(ns).Get(upgradePodName)
	if err != nil {
		return err
	}

	for _, store := range cc.Status.Store.Stores {
		if store.PodName == upgradePodName {
			storeID, err := strconv.ParseUint(store.ID, 10, 64)
			if err != nil {
				return err
			}
			_, evicting := upgradePod.Annotations[EvictLeaderBeginTime]

			if csu.readyToUpgrade(upgradePod, store) {
				setUpgradePartition(newSet, ordinal)
				return nil
			}

			if !evicting {
				return csu.beginEvictLeader(cc, storeID, upgradePod)
			}
			return controller.RequeueErrorf("cellcluster: [%s/%s]'s store pod: [%s] is evicting leader", ns, ccName, upgradePodName)
		}
	}

	return controller.RequeueErrorf("cellcluster: [%s/%s] no store status found for store pod: [%s]", ns, ccName, upgradePodName)
}

func (csu *storeUpgrader) readyToUpgrade(upgradePod *corev1.Pod, store v1alpha1.KVStore) bool {
	if store.LeaderCount == 0 {
		return true
	}
	if evictLeaderBeginTimeStr, evicting := upgradePod.Annotations[EvictLeaderBeginTime]; evicting {
		evictLeaderBeginTime, err := time.Parse(time.RFC3339, evictLeaderBeginTimeStr)
		if err != nil {
			glog.Errorf("parse annotation:[%s] to time failed.", EvictLeaderBeginTime)
			return false
		}
		if time.Now().After(evictLeaderBeginTime.Add(EvictLeaderTimeout)) {
			return true
		}
	}
	return false
}

func (csu *storeUpgrader) beginEvictLeader(cc *v1alpha1.CellCluster, storeID uint64, pod *corev1.Pod) error {
	err := csu.pdControl.GetPDClient(cc).BeginEvictLeader(storeID)
	if err != nil {
		return err
	}
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[EvictLeaderBeginTime] = time.Now().Format(time.RFC3339)
	_, err = csu.podControl.UpdatePod(cc, pod)
	return err
}

func (csu *storeUpgrader) endEvictLeader(cc *v1alpha1.CellCluster, ordinal int32) error {
	store := csu.getStoreByOrdinal(cc, ordinal)
	storeID, err := strconv.ParseUint(store.ID, 10, 64)
	if err != nil {
		return err
	}
	upgradedPodName := storePodName(cc.GetName(), ordinal)
	upgradedPod, err := csu.podLister.Pods(cc.GetNamespace()).Get(upgradedPodName)
	if err != nil {
		return err
	}
	_, evicting := upgradedPod.Annotations[EvictLeaderBeginTime]
	if evicting {
		delete(upgradedPod.Annotations, EvictLeaderBeginTime)
		_, err = csu.podControl.UpdatePod(cc, upgradedPod)
		if err != nil {
			return err
		}
	}
	err = csu.pdControl.GetPDClient(cc).EndEvictLeader(storeID)
	if err != nil {
		return err
	}
	return nil
}

func (csu *storeUpgrader) getStoreByOrdinal(cc *v1alpha1.CellCluster, ordinal int32) *v1alpha1.KVStore {
	podName := storePodName(cc.GetName(), ordinal)
	for _, store := range cc.Status.Store.Stores {
		if store.PodName == podName {
			return &store
		}
	}
	return nil
}

type fakeStoreUpgrader struct{}

// NewFakeStoreUpgrader returns a fake store upgrader
func NewFakeStoreUpgrader() Upgrader {
	return &fakeStoreUpgrader{}
}

func (csu *fakeStoreUpgrader) Upgrade(cc *v1alpha1.CellCluster, _ *apps.StatefulSet, _ *apps.StatefulSet) error {
	cc.Status.Store.Phase = v1alpha1.UpgradePhase
	return nil
}
