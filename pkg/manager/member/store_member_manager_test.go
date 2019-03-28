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
	"strings"
	"testing"
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned/fake"
	informers "github.com/deepfabric/elasticell-operator/pkg/client/informers/externalversions"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	metapb "github.com/deepfabric/elasticell/pkg/pb/metapb"
	"github.com/deepfabric/elasticell/pkg/pdapi"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestStoreMemberManagerSyncCreate(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                     string
		prepare                  func(cluster *v1alpha1.CellCluster)
		errWhenCreateStatefulSet bool
		errWhenGetStores         bool
		err                      bool
		setCreated               bool
		pdStores                 *controller.StoresInfo
		tombstoneStores          *controller.StoresInfo
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellClusterForPD()
		cc.Status.PD.Members = map[string]v1alpha1.PDMember{
			"pd-0": {Name: "pd-0", Health: true},
			"pd-1": {Name: "pd-1", Health: true},
			"pd-2": {Name: "pd-2", Health: true},
		}
		cc.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}

		ns := cc.Namespace
		ccName := cc.Name
		oldSpec := cc.Spec
		if test.prepare != nil {
			test.prepare(cc)
		}

		stmm, fakeSetControl, _, pdClient, _, _ := newFakeStoreMemberManager(cc)

		if test.errWhenGetStores {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get stores from store cluster")
			})
		} else {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.pdStores, nil
			})
			pdClient.AddReaction(controller.GetTombStoneStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.tombstoneStores, nil
			})
			pdClient.AddReaction(controller.SetStoreLabelsActionType, func(action *controller.Action) (interface{}, error) {
				return true, nil
			})
		}

		if test.errWhenCreateStatefulSet {
			fakeSetControl.SetCreateStatefulSetError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err := stmm.Sync(cc)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}

		g.Expect(cc.Spec).To(Equal(oldSpec))

		tc1, err := stmm.setLister.StatefulSets(ns).Get(controller.StoreMemberName(ccName))
		if test.setCreated {
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(tc1).NotTo(Equal(nil))
		} else {
			expectErrIsNotFound(g, err)
		}
	}

	tests := []testcase{
		{
			name:                     "normal",
			prepare:                  nil,
			errWhenCreateStatefulSet: false,
			err:                      false,
			setCreated:               true,
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
		},
		{
			name: "pd is not available",
			prepare: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Members = map[string]v1alpha1.PDMember{}
			},
			errWhenCreateStatefulSet: false,
			err:                      true,
			setCreated:               false,
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
		},
		{
			name: "cellcluster's storage format is wrong",
			prepare: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Store.Requests.Storage = "100xxxxi"
			},
			errWhenCreateStatefulSet: false,
			err:                      true,
			setCreated:               false,
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
		},
		{
			name:                     "error when create statefulset",
			prepare:                  nil,
			errWhenCreateStatefulSet: true,
			err:                      true,
			setCreated:               false,
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestStoreMemberManagerSyncUpdate(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                     string
		modify                   func(cluster *v1alpha1.CellCluster)
		pdStores                 *controller.StoresInfo
		tombstoneStores          *controller.StoresInfo
		errWhenUpdateStatefulSet bool
		errWhenGetStores         bool
		statusChange             func(*apps.StatefulSet)
		err                      bool
		expectStatefulSetFn      func(*GomegaWithT, *apps.StatefulSet, error)
		expectCellClusterFn      func(*GomegaWithT, *v1alpha1.CellCluster)
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellClusterForPD()
		cc.Status.PD.Members = map[string]v1alpha1.PDMember{
			"pd-0": {Name: "pd-0", Health: true},
			"pd-1": {Name: "pd-1", Health: true},
			"pd-2": {Name: "pd-2", Health: true},
		}
		cc.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}

		ns := cc.Namespace
		ccName := cc.Name

		stmm, fakeSetControl, _, pdClient, _, _ := newFakeStoreMemberManager(cc)
		if test.errWhenGetStores {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get stores from pd cluster")
			})
		} else {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.pdStores, nil
			})
			pdClient.AddReaction(controller.GetTombStoneStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.tombstoneStores, nil
			})
			pdClient.AddReaction(controller.SetStoreLabelsActionType, func(action *controller.Action) (interface{}, error) {
				return true, nil
			})
		}

		if test.statusChange == nil {
			fakeSetControl.SetStatusChange(func(set *apps.StatefulSet) {
				set.Status.Replicas = *set.Spec.Replicas
				set.Status.CurrentRevision = "pd-1"
				set.Status.UpdateRevision = "pd-1"
				observedGeneration := int64(1)
				set.Status.ObservedGeneration = &observedGeneration
			})
		} else {
			fakeSetControl.SetStatusChange(test.statusChange)
		}

		err := stmm.Sync(cc)
		g.Expect(err).NotTo(HaveOccurred())

		_, err = stmm.setLister.StatefulSets(ns).Get(controller.StoreMemberName(ccName))
		g.Expect(err).NotTo(HaveOccurred())

		tc1 := cc.DeepCopy()
		test.modify(tc1)

		if test.errWhenUpdateStatefulSet {
			fakeSetControl.SetUpdateStatefulSetError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err = stmm.Sync(tc1)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}

		if test.expectStatefulSetFn != nil {
			set, err := stmm.setLister.StatefulSets(ns).Get(controller.StoreMemberName(ccName))
			test.expectStatefulSetFn(g, set, err)
		}
		if test.expectCellClusterFn != nil {
			test.expectCellClusterFn(g, tc1)
		}
	}

	tests := []testcase{
		{
			name: "normal",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Store.Replicas = 5
				cc.Spec.Services = []v1alpha1.Service{
					{Name: "store", Type: string(corev1.ServiceTypeNodePort)},
				}
				cc.Status.PD.Phase = v1alpha1.NormalPhase
			},
			// TODO add unit test for status sync
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			errWhenUpdateStatefulSet: false,
			errWhenGetStores:         false,
			err:                      false,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(int(*set.Spec.Replicas)).To(Equal(4))
			},
			expectCellClusterFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(*cc.Status.Store.StatefulSet.ObservedGeneration).To(Equal(int64(1)))
				g.Expect(cc.Status.Store.Stores).To(Equal(map[string]v1alpha1.KVStore{}))
				g.Expect(cc.Status.Store.TombstoneStores).To(Equal(map[string]v1alpha1.KVStore{}))
			},
		},
		{
			name: "cellcluster's storage format is wrong",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Store.Requests.Storage = "100xxxxi"
				cc.Status.PD.Phase = v1alpha1.NormalPhase
			},
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			errWhenUpdateStatefulSet: false,
			err:                      true,
			expectStatefulSetFn:      nil,
		},
		{
			name: "error when update statefulset",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Store.Replicas = 5
				cc.Status.PD.Phase = v1alpha1.NormalPhase
			},
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			errWhenUpdateStatefulSet: true,
			err:                      true,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
		},
		{
			name: "error when sync store status",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Store.Replicas = 5
				cc.Status.PD.Phase = v1alpha1.NormalPhase
			},
			pdStores:                 &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			tombstoneStores:          &controller.StoresInfo{Count: 0, Stores: []*pdapi.StoreInfo{}},
			errWhenUpdateStatefulSet: false,
			errWhenGetStores:         true,
			err:                      true,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(int(*set.Spec.Replicas)).To(Equal(3))
			},
			expectCellClusterFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(0))
			},
		},
	}

	for i := range tests {
		t.Logf("begin: %s", tests[i].name)
		testFn(&tests[i], t)
		t.Logf("end: %s", tests[i].name)
	}
}

func TestStoreMemberManagerStoreStatefulSetIsUpgrading(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name             string
		setUpdate        func(*apps.StatefulSet)
		hasPod           bool
		updatePod        func(*corev1.Pod)
		errWhenGetLeader bool
		hasLeader        bool
		errExpectFn      func(*GomegaWithT, error)
		expectUpgrading  bool
	}
	testFn := func(test *testcase, t *testing.T) {
		cc := newCellClusterForPD()
		pdmm, _, _, pdClient, podIndexer, _ := newFakeStoreMemberManager(cc)
		cc.Status.Store.StatefulSet = &apps.StatefulSetStatus{
			UpdateRevision: "v3",
		}

		set := &apps.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: metav1.NamespaceDefault,
			},
		}
		if test.setUpdate != nil {
			test.setUpdate(set)
		}

		if test.hasPod {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        ordinalPodName(v1alpha1.StoreMemberType, cc.GetName(), 0),
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{},
					Labels:      label.New().Instance(cc.GetLabels()[label.InstanceLabelKey]).Store().Labels(),
				},
			}
			if test.updatePod != nil {
				test.updatePod(pod)
			}
			podIndexer.Add(pod)
		}
		if test.errWhenGetLeader {
			pdClient.AddReaction(controller.GetEvictLeaderSchedulersActionType, func(action *controller.Action) (interface{}, error) {
				return []string{}, fmt.Errorf("failed to get leader")
			})
		} else if test.hasLeader {
			pdClient.AddReaction(controller.GetEvictLeaderSchedulersActionType, func(action *controller.Action) (interface{}, error) {
				return []string{"leader"}, nil
			})
		} else {
			pdClient.AddReaction(controller.GetEvictLeaderSchedulersActionType, func(action *controller.Action) (interface{}, error) {
				return []string{}, nil
			})
		}
		b, err := pdmm.storeStatefulSetIsUpgradingFn(pdmm.podLister, pdmm.pdControl, set, cc)
		if test.errExpectFn != nil {
			test.errExpectFn(g, err)
		}
		if test.expectUpgrading {
			g.Expect(b).To(BeTrue())
		} else {
			g.Expect(b).NotTo(BeTrue())
		}
	}
	tests := []testcase{
		{
			name: "stateful set is upgrading",
			setUpdate: func(set *apps.StatefulSet) {
				set.Status.CurrentRevision = "v1"
				set.Status.UpdateRevision = "v2"
				set.Status.ObservedGeneration = func() *int64 { var i int64; i = 1000; return &i }()
			},
			hasPod:           false,
			updatePod:        nil,
			errWhenGetLeader: false,
			hasLeader:        false,
			errExpectFn:      errExpectNil,
			expectUpgrading:  true,
		},
		{
			name:             "pod don't have revision hash",
			setUpdate:        nil,
			hasPod:           true,
			updatePod:        nil,
			errWhenGetLeader: false,
			hasLeader:        false,
			errExpectFn:      errExpectNil,
			expectUpgrading:  false,
		},
		{
			name:      "pod have revision hash, not equal statefulset's",
			setUpdate: nil,
			hasPod:    true,
			updatePod: func(pod *corev1.Pod) {
				pod.Labels[apps.ControllerRevisionHashLabelKey] = "v2"
			},
			errWhenGetLeader: false,
			hasLeader:        false,
			errExpectFn:      errExpectNil,
			expectUpgrading:  true,
		},
		{
			name:      "pod have revision hash, equal statefulset's",
			setUpdate: nil,
			hasPod:    true,
			updatePod: func(pod *corev1.Pod) {
				pod.Labels[apps.ControllerRevisionHashLabelKey] = "v3"
			},
			errWhenGetLeader: false,
			hasLeader:        false,
			errExpectFn:      errExpectNil,
			expectUpgrading:  false,
		},
		/*
			{
				name:      "has no leader",
				setUpdate: nil,
				hasPod:    true,
				updatePod: func(pod *corev1.Pod) {
					pod.Labels[apps.ControllerRevisionHashLabelKey] = "v3"
				},
				errWhenGetLeader: false,
				hasLeader:        false,
				errExpectFn:      errExpectNil,
				expectUpgrading:  false,
			},
			{
				name:      "has leader",
				setUpdate: nil,
				hasPod:    true,
				updatePod: func(pod *corev1.Pod) {
					pod.Labels[apps.ControllerRevisionHashLabelKey] = "v3"
				},
				errWhenGetLeader: false,
				hasLeader:        true,
				errExpectFn:      errExpectNil,
				expectUpgrading:  true,
			},
		*/
	}

	for i := range tests {
		t.Logf(tests[i].name)
		testFn(&tests[i], t)
	}
}

/*
func TestStoreMemberManagerSetStoreLabelsForStore(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name             string
		errWhenGetStores bool
		hasNode          bool
		hasPod           bool
		storeInfo        *controller.StoresInfo
		errExpectFn      func(*GomegaWithT, error)
		setCount         int
		labelSetFailed   bool
	}
	testFn := func(test *testcase, t *testing.T) {
		cc := newCellClusterForPD()
		pdmm, _, _, pdClient, podIndexer, nodeIndexer := newFakeStoreMemberManager(cc)

		if test.errWhenGetStores {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get stores")
			})
		}
		if test.storeInfo != nil {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.storeInfo, nil
			})
		}
		if test.hasNode {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node-1",
					Labels: map[string]string{
						"region":           "region",
						"zone":             "zone",
						"rack":             "rack",
						apis.LabelHostname: "host",
					},
				},
			}
			nodeIndexer.Add(node)
		}
		if test.hasPod {
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: metav1.NamespaceDefault,
				},
				Spec: corev1.PodSpec{
					NodeName: "node-1",
				},
			}
			podIndexer.Add(pod)
		}
		if test.labelSetFailed {
			pdClient.AddReaction(controller.SetStoreLabelsActionType, func(action *controller.Action) (interface{}, error) {
				return false, fmt.Errorf("label set failed")
			})
		} else {
			pdClient.AddReaction(controller.SetStoreLabelsActionType, func(action *controller.Action) (interface{}, error) {
				return true, nil
			})
		}

		setCount, err := pdmm.setStoreLabelsForStore(cc)
		if test.errExpectFn != nil {
			test.errExpectFn(g, err)
		}
		g.Expect(setCount).To(Equal(test.setCount))
	}
	tests := []testcase{
		{
			name:             "get stores return error",
			errWhenGetStores: true,
			storeInfo:        nil,
			hasNode:          true,
			hasPod:           true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "failed to get stores")).To(BeTrue())
			},
			setCount:       0,
			labelSetFailed: false,
		},
		{
			name:             "stores is empty",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			hasNode: true,
			hasPod:  true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			setCount:       0,
			labelSetFailed: false,
		},
		{
			name:             "status is nil",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					{
						Status: nil,
					},
				},
			},
			hasNode: true,
			hasPod:  true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			setCount:       0,
			labelSetFailed: false,
		},
		{
			name:             "store is nil",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					nil,
				},
			},
			hasNode: true,
			hasPod:  true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			setCount:       0,
			labelSetFailed: false,
		},
		{
			name:             "don't have pod",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Count: 1,
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{ID: 1, State: metapb.UP},
					},
				},
			},
			hasNode: true,
			hasPod:  false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "not found")).To(BeTrue())
			},
			setCount:       0,
			labelSetFailed: false,
		},
		{
			name:             "don't have node",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta:   metapb.Store{ID: 333, State: metapb.UP, Address: "pod-1.ns-1"},
						Status: &pdapi.StoreStatus{LeaderCount: 1, LastHeartbeatTS: time.Now().Unix()},
					},
				},
			},
			hasNode: false,
			hasPod:  true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			setCount:       0,
			labelSetFailed: false,
		},
		{
			name:             "labels not equal, but set failed",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					{
						Store: &controller.MetaStore{
							Store: &metapb.Store{
								Id:      333,
								Address: "pod-1.ns-1",
								Labels: []*metapb.StoreLabel{
									{
										Key:   "region",
										Value: "region",
									},
								},
							},
							StateName: "Up",
						},
						Status: &controller.StoreStatus{
							LeaderCount:     1,
							LastHeartbeatTS: time.Now(),
						},
					},
				},
			},
			hasNode: true,
			hasPod:  true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			setCount:       0,
			labelSetFailed: true,
		},
		{
			name:             "labels not equal, set success",
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					{
						Store: &controller.MetaStore{
							Store: &metapb.Store{
								Id:      333,
								Address: "pod-1.ns-1",
								Labels: []*metapb.StoreLabel{
									{
										Key:   "region",
										Value: "region",
									},
								},
							},
							StateName: "Up",
						},
						Status: &controller.StoreStatus{
							LeaderCount:     1,
							LastHeartbeatTS: time.Now(),
						},
					},
				},
			},
			hasNode: true,
			hasPod:  true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			setCount:       1,
			labelSetFailed: false,
		},
	}

	for i := range tests {
		t.Logf(tests[i].name)
		testFn(&tests[i], t)
	}
}
*/
func TestStoreMemberManagerSyncCellClusterStatus(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                      string
		updateCC                  func(*v1alpha1.CellCluster)
		upgradingFn               func(corelisters.PodLister, controller.PDControlInterface, *apps.StatefulSet, *v1alpha1.CellCluster) (bool, error)
		errWhenGetStores          bool
		storeInfo                 *controller.StoresInfo
		errWhenGetTombstoneStores bool
		tombstoneStoreInfo        *controller.StoresInfo
		errExpectFn               func(*GomegaWithT, error)
		ccExpectFn                func(*GomegaWithT, *v1alpha1.CellCluster)
	}
	status := apps.StatefulSetStatus{
		Replicas: int32(3),
	}
	now := metav1.Time{Time: time.Now()}
	testFn := func(test *testcase, t *testing.T) {
		cc := newCellClusterForPD()
		cc.Status.PD.Phase = v1alpha1.NormalPhase
		set := &apps.StatefulSet{
			Status: status,
		}
		if test.updateCC != nil {
			test.updateCC(cc)
		}
		pdmm, _, _, pdClient, _, _ := newFakeStoreMemberManager(cc)

		if test.upgradingFn != nil {
			pdmm.storeStatefulSetIsUpgradingFn = test.upgradingFn
		}
		if test.errWhenGetStores {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get stores")
			})
		} else if test.storeInfo != nil {
			pdClient.AddReaction(controller.GetStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.storeInfo, nil
			})
		}
		if test.errWhenGetTombstoneStores {
			pdClient.AddReaction(controller.GetTombStoneStoresActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get tombstone stores")
			})
		} else if test.tombstoneStoreInfo != nil {
			pdClient.AddReaction(controller.GetTombStoneStoresActionType, func(action *controller.Action) (interface{}, error) {
				return test.tombstoneStoreInfo, nil
			})
		}

		err := pdmm.syncCellClusterStatus(cc, set)
		if test.errExpectFn != nil {
			test.errExpectFn(g, err)
		}
		if test.ccExpectFn != nil {
			test.ccExpectFn(g, cc)
		}
	}
	tests := []testcase{
		{
			name:     "whether statefulset is upgrading returns failed",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, fmt.Errorf("whether upgrading failed")
			},
			errWhenGetStores:          false,
			storeInfo:                 nil,
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo:        nil,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "whether upgrading failed")).To(BeTrue())
			},
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Store.StatefulSet.Replicas).To(Equal(int32(3)))
			},
		},
		{
			name:     "statefulset is upgrading",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return true, nil
			},
			errWhenGetStores:          false,
			storeInfo:                 nil,
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo:        nil,
			errExpectFn:               nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Store.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Store.Phase).To(Equal(v1alpha1.UpgradePhase))
			},
		},
		{
			name: "statefulset is upgrading but pd is upgrading",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.UpgradePhase
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return true, nil
			},
			errWhenGetStores:          false,
			storeInfo:                 nil,
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo:        nil,
			errExpectFn:               nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Store.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Store.Phase).To(Equal(v1alpha1.NormalPhase))
			},
		},
		{
			name:     "statefulset is not upgrading",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores:          false,
			storeInfo:                 nil,
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo:        nil,
			errExpectFn:               nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Store.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Store.Phase).To(Equal(v1alpha1.NormalPhase))
			},
		},
		{
			name:     "get stores failed",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores:          true,
			storeInfo:                 nil,
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo:        nil,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "failed to get stores")).To(BeTrue())
			},
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Store.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Store.Synced).To(BeFalse())
			},
		},
		{
			name:     "stores is empty",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(0))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name:     "store is nil",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					nil,
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(0))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name:     "status is nil",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					{
						Status: nil,
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(0))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name: "LastHeartbeatTS is zero, CellClulster LastHeartbeatTS is not zero",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
				cc.Status.Store.Stores["333"] = v1alpha1.KVStore{
					LastHeartbeatTime: now,
				}
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: 0,
						},
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(time.Time{}.IsZero()).To(BeTrue())
				g.Expect(cc.Status.Store.Stores["333"].LastHeartbeatTime.Time.IsZero()).To(BeFalse())
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name: "LastHeartbeatTS is zero, CellClulster LastHeartbeatTS is zero",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
				cc.Status.Store.Stores["333"] = v1alpha1.KVStore{
					LastHeartbeatTime: metav1.Time{Time: time.Time{}},
				}
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: 0,
						},
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(time.Time{}.IsZero()).To(BeTrue())
				g.Expect(cc.Status.Store.Stores["333"].LastHeartbeatTime.Time.IsZero()).To(BeTrue())
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name: "LastHeartbeatTS is not zero, CellClulster LastHeartbeatTS is zero",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
				cc.Status.Store.Stores["333"] = v1alpha1.KVStore{
					LastHeartbeatTime: metav1.Time{Time: time.Time{}},
				}
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: time.Now().Unix(),
						},
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(time.Time{}.IsZero()).To(BeTrue())
				g.Expect(cc.Status.Store.Stores["333"].LastHeartbeatTime.Time.IsZero()).To(BeFalse())
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name: "set LastTransitionTime first time",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
				// cc.Status.Store.Stores["333"] = v1alpha1.KVStore{}
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: time.Now().Unix(),
						},
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(cc.Status.Store.Stores["333"].LastTransitionTime.Time.IsZero()).To(BeFalse())
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		/*
			{
				name: "state not change, LastTransitionTime not change",
				updateCC: func(cc *v1alpha1.CellCluster) {
					cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
					cc.Status.Store.Stores["333"] = v1alpha1.KVStore{
						LastTransitionTime: now,
						State:              v1alpha1.StoreStateUp,
					}
				},
				upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
					return false, nil
				},
				errWhenGetStores: false,
				storeInfo: &controller.StoresInfo{
					Stores: []*pdapi.StoreInfo{
						&pdapi.StoreInfo{
							Meta: metapb.Store{
								ID:      333,
								Address: "pod-1.ns-1",
							},
							Status: &pdapi.StoreStatus{
								LastHeartbeatTS: now.UnixNano(),
							},
						},
					},
				},
				errWhenGetTombstoneStores: false,
				tombstoneStoreInfo: &controller.StoresInfo{
					Stores: []*pdapi.StoreInfo{},
				},
				errExpectFn: errExpectNil,
				ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
					g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
					g.Expect(cc.Status.Store.Stores["333"].LastTransitionTime).To(Equal(now))
					g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
					g.Expect(cc.Status.Store.Synced).To(BeTrue())
				},
			},
		*/
		{
			name: "state change, LastTransitionTime change",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
				cc.Status.Store.Stores["333"] = v1alpha1.KVStore{
					LastTransitionTime: now,
					State:              v1alpha1.StoreStateUp,
				}
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					{
						/*
							Store: &controller.MetaStore{
								Store: &metapb.Store{
									Id:      333,
									Address: "pod-1.ns-1",
								},
								StateName: "Down",
							},
							Status: &controller.StoreStatus{
								LastHeartbeatTS: time.Now(),
							},
						*/
					},
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
							State:   metapb.Down,
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: time.Now().Unix(),
						},
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(cc.Status.Store.Stores["333"].LastTransitionTime).NotTo(Equal(now))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
		{
			name: "get tombstone stores failed",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
				cc.Status.Store.Stores["333"] = v1alpha1.KVStore{
					LastTransitionTime: now,
					State:              v1alpha1.StoreStateUp,
				}
			},
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					{
						/*
							Store: &controller.MetaStore{
								Store: &metapb.Store{
									Id:      333,
									Address: "pod-1.ns-1",
								},
								StateName: "Up",
							},
							Status: &controller.StoreStatus{
								LastHeartbeatTS: time.Now(),
							},
						*/
					},
				},
			},
			errWhenGetTombstoneStores: true,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{},
			},
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "failed to get tombstone stores")).To(BeTrue())
			},
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(0))
				g.Expect(cc.Status.Store.Synced).To(BeFalse())
			},
		},
		{
			name:     "all works",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, controlInterface controller.PDControlInterface, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errWhenGetStores: false,
			storeInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
							State:   metapb.UP,
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: time.Now().Unix(),
						},
					},
				},
			},
			errWhenGetTombstoneStores: false,
			tombstoneStoreInfo: &controller.StoresInfo{
				Stores: []*pdapi.StoreInfo{
					&pdapi.StoreInfo{
						Meta: metapb.Store{
							ID:      333,
							Address: "pod-1.ns-1",
							State:   metapb.Tombstone,
						},
						Status: &pdapi.StoreStatus{
							LastHeartbeatTS: time.Now().Unix(),
						},
					},
				},
			},
			errExpectFn: errExpectNil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(len(cc.Status.Store.Stores)).To(Equal(1))
				g.Expect(len(cc.Status.Store.TombstoneStores)).To(Equal(1))
				g.Expect(cc.Status.Store.Synced).To(BeTrue())
			},
		},
	}

	for i := range tests {
		t.Logf(tests[i].name)
		testFn(&tests[i], t)
	}
}

func newFakeStoreMemberManager(cc *v1alpha1.CellCluster) (
	*storeMemberManager, *controller.FakeStatefulSetControl,
	*controller.FakeServiceControl, *controller.FakePDClient, cache.Indexer, cache.Indexer) {
	cli := fake.NewSimpleClientset()
	kubeCli := kubefake.NewSimpleClientset()
	pdControl := controller.NewFakePDControl()
	pdClient := controller.NewFakePDClient()
	pdControl.SetPDClient(cc, pdClient)
	setInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Apps().V1beta1().StatefulSets()
	svcInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Services()
	tcInformer := informers.NewSharedInformerFactory(cli, 0).Deepfabric().V1alpha1().CellClusters()
	setControl := controller.NewFakeStatefulSetControl(setInformer, tcInformer)
	svcControl := controller.NewFakeServiceControl(svcInformer, tcInformer)
	podInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Pods()
	nodeInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Nodes()
	storeScaler := NewFakeStoreScaler()
	storeUpgrader := NewFakeStoreUpgrader()

	pxmm := &storeMemberManager{
		pdControl:     pdControl,
		podLister:     podInformer.Lister(),
		nodeLister:    nodeInformer.Lister(),
		setControl:    setControl,
		svcControl:    svcControl,
		setLister:     setInformer.Lister(),
		svcLister:     svcInformer.Lister(),
		storeScaler:   storeScaler,
		storeUpgrader: storeUpgrader,
	}
	pxmm.storeStatefulSetIsUpgradingFn = storeStatefulSetIsUpgrading
	return pxmm, setControl, svcControl, pdClient, podInformer.Informer().GetIndexer(), nodeInformer.Informer().GetIndexer()
}
