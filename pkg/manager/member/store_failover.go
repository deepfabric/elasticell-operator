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
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
)

type storeFailover struct {
	pdControl controller.PDControlInterface
}

// NewStoreFailover returns a store Failover
func NewStoreFailover(pdControl controller.PDControlInterface) Failover {
	return &storeFailover{pdControl}
}

func (sf *storeFailover) Failover(cc *v1alpha1.CellCluster) error {
	maxStoreDownTimeDuration, err := sf.pdControl.GetPDClient(cc).GetConfig()
	if err != nil {
		return err
	}

	for storeID, store := range cc.Status.Store.Stores {
		podName := store.PodName
		if store.LastTransitionTime.IsZero() {
			continue
		}
		deadline := store.LastTransitionTime.Add(maxStoreDownTimeDuration)
		exist := false
		for _, failureStore := range cc.Status.Store.FailureStores {
			if failureStore.PodName == podName {
				exist = true
				break
			}
		}
		if store.State == v1alpha1.StoreStateDown && time.Now().After(deadline) && !exist {
			if cc.Status.Store.FailureStores == nil {
				cc.Status.Store.FailureStores = map[string]v1alpha1.KVFailureStore{}
			}
			cc.Status.Store.FailureStores[storeID] = v1alpha1.KVFailureStore{
				PodName: podName,
				StoreID: store.ID,
			}
		}
	}

	return nil
}

func (sf *storeFailover) Recover(_ *v1alpha1.CellCluster) {
	// Do nothing now
}

type fakeTiKVFailover struct{}

// NewFakeStoreFailover returns a fake Failover
func NewFakeStoreFailover() Failover {
	return &fakeTiKVFailover{}
}

func (ftf *fakeTiKVFailover) Failover(_ *v1alpha1.CellCluster) error {
	return nil
}

func (ftf *fakeTiKVFailover) Recover(_ *v1alpha1.CellCluster) {
	return
}
