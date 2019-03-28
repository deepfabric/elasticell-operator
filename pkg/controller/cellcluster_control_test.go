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

package controller

import (
	"errors"
	"testing"

	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned/fake"
	listers "github.com/deepfabric/elasticell-operator/pkg/client/listers/deepfabric.com/v1alpha1"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

func TestCellClusterControlUpdateCellCluster(t *testing.T) {
	g := NewGomegaWithT(t)
	recorder := record.NewFakeRecorder(10)
	cc := newCellCluster()
	cc.Spec.PD.Replicas = int32(5)
	fakeClient := &fake.Clientset{}
	control := NewRealCellClusterControl(fakeClient, nil, recorder)
	fakeClient.AddReactor("update", "cellclusters", func(action core.Action) (bool, runtime.Object, error) {
		update := action.(core.UpdateAction)
		return true, update.GetObject(), nil
	})
	updateCC, err := control.UpdateCellCluster(cc, &v1alpha1.CellClusterStatus{}, &v1alpha1.CellClusterStatus{})
	g.Expect(err).To(Succeed())
	g.Expect(updateCC.Spec.PD.Replicas).To(Equal(int32(5)))

	events := collectEvents(recorder.Events)
	g.Expect(events).To(HaveLen(0))
}

func TestCellClusterControlUpdateCellClusterConflictSuccess(t *testing.T) {
	g := NewGomegaWithT(t)
	recorder := record.NewFakeRecorder(10)
	cc := newCellCluster()
	fakeClient := &fake.Clientset{}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	ccLister := listers.NewCellClusterLister(indexer)
	control := NewRealCellClusterControl(fakeClient, ccLister, recorder)
	conflict := false
	fakeClient.AddReactor("update", "cellclusters", func(action core.Action) (bool, runtime.Object, error) {
		update := action.(core.UpdateAction)
		if !conflict {
			conflict = true
			return true, update.GetObject(), apierrors.NewConflict(action.GetResource().GroupResource(), cc.Name, errors.New("conflict"))
		}
		return true, update.GetObject(), nil
	})
	_, err := control.UpdateCellCluster(cc, &v1alpha1.CellClusterStatus{}, &v1alpha1.CellClusterStatus{})
	g.Expect(err).To(Succeed())

	events := collectEvents(recorder.Events)
	g.Expect(events).To(HaveLen(0))
}

func TestDeepEqualExceptHeartbeatTime(t *testing.T) {
	g := NewGomegaWithT(t)

	new := &v1alpha1.CellClusterStatus{
		Store: v1alpha1.StoreStatus{
			Synced: true,
			Stores: map[string]v1alpha1.KVStore{
				"1": {
					LastHeartbeatTime: metav1.Now(),
					ID:                "1",
				},
			},
		},
	}
	time.Sleep(1 * time.Second)
	old := &v1alpha1.CellClusterStatus{
		Store: v1alpha1.StoreStatus{
			Synced: true,
			Stores: map[string]v1alpha1.KVStore{
				"1": {
					LastHeartbeatTime: metav1.Now(),
					ID:                "1",
				},
			},
		},
	}
	g.Expect(deepEqualExceptHeartbeatTime(new, old)).To(Equal(true))
}
