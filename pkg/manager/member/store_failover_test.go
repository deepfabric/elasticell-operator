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
	"testing"
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStoreFailoverFailover(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name      string
		update    func(*v1alpha1.CellCluster)
		getCfgErr bool
		err       bool
		expectFn  func(*v1alpha1.CellCluster)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)
		cc := newCellClusterForPD()
		test.update(cc)
		storeFailover, fakePDControl := newFakeStoreFailover()
		pdClient := controller.NewFakePDClient()
		fakePDControl.SetPDClient(cc, pdClient)

		pdClient.AddReaction(controller.GetConfigActionType, func(action *controller.Action) (interface{}, error) {
			if test.getCfgErr {
				return nil, fmt.Errorf("get config failed")
			}
			return controller.MaxStoreDownTimeDuration, nil
		})

		err := storeFailover.Failover(cc)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}
		test.expectFn(cc)
	}

	tests := []testcase{
		{
			name: "normal",
			update: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"1": {
						State:              v1alpha1.StoreStateDown,
						PodName:            "store-1",
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-70 * time.Minute)},
					},
					"2": {
						State:              v1alpha1.StoreStateDown,
						PodName:            "store-2",
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-61 * time.Minute)},
					},
				}
			},
			getCfgErr: false,
			err:       false,
			expectFn: func(cc *v1alpha1.CellCluster) {
				g.Expect(int(cc.Spec.Store.Replicas)).To(Equal(3))
				g.Expect(len(cc.Status.Store.FailureStores)).To(Equal(2))
			},
		},
		{
			name:      "get config failed",
			update:    func(*v1alpha1.CellCluster) {},
			getCfgErr: true,
			err:       true,
			expectFn: func(cc *v1alpha1.CellCluster) {
				g.Expect(int(cc.Spec.Store.Replicas)).To(Equal(3))
				g.Expect(len(cc.Status.Store.FailureStores)).To(Equal(0))
			},
		},
		{
			name: "store state is not Down",
			update: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"1": {State: v1alpha1.StoreStateUp, PodName: "store-1"},
				}
			},
			getCfgErr: false,
			err:       false,
			expectFn: func(cc *v1alpha1.CellCluster) {
				g.Expect(int(cc.Spec.Store.Replicas)).To(Equal(3))
				g.Expect(len(cc.Status.Store.FailureStores)).To(Equal(0))
			},
		},
		{
			name: "deadline not exceed",
			update: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"1": {
						State:              v1alpha1.StoreStateDown,
						PodName:            "store-1",
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-30 * time.Minute)},
					},
				}
			},
			getCfgErr: false,
			err:       false,
			expectFn: func(cc *v1alpha1.CellCluster) {
				g.Expect(int(cc.Spec.Store.Replicas)).To(Equal(3))
				g.Expect(len(cc.Status.Store.FailureStores)).To(Equal(0))
			},
		},
		{
			name: "lastTransitionTime is zero",
			update: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"1": {
						State:   v1alpha1.StoreStateDown,
						PodName: "store-1",
					},
				}
			},
			getCfgErr: false,
			err:       false,
			expectFn: func(cc *v1alpha1.CellCluster) {
				g.Expect(int(cc.Spec.Store.Replicas)).To(Equal(3))
				g.Expect(len(cc.Status.Store.FailureStores)).To(Equal(0))
			},
		},
		{
			name: "exist in failureStores",
			update: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"1": {
						State:              v1alpha1.StoreStateDown,
						PodName:            "store-1",
						LastTransitionTime: metav1.Time{Time: time.Now().Add(-70 * time.Minute)},
					},
				}
				cc.Status.Store.FailureStores = map[string]v1alpha1.KVFailureStore{
					"1": {
						PodName: "store-1",
						StoreID: "1",
					},
				}
			},
			getCfgErr: false,
			err:       false,
			expectFn: func(cc *v1alpha1.CellCluster) {
				g.Expect(int(cc.Spec.Store.Replicas)).To(Equal(3))
				g.Expect(len(cc.Status.Store.FailureStores)).To(Equal(1))
			},
		},
	}
	for i := range tests {
		testFn(&tests[i], t)
	}
}

func newFakeStoreFailover() (*storeFailover, *controller.FakePDControl) {
	pdControl := controller.NewFakePDControl()
	return &storeFailover{pdControl}, pdControl
}
