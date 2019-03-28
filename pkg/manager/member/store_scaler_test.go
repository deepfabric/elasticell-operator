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
	"github.com/deepfabric/elasticell-operator/pkg/label"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func newStatefulSetForPDScale() *apps.StatefulSet {
	set := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scaler",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: int32Pointer(5),
		},
	}
	return set
}
func TestStoreScalerScaleOut(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name           string
		storeUpgrading bool
		hasPVC         bool
		hasDeferAnn    bool
		pvcDeleteErr   bool
		errExpectFn    func(*GomegaWithT, error)
		changed        bool
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)
		cc := newCellClusterForPD()

		if test.storeUpgrading {
			cc.Status.Store.Phase = v1alpha1.UpgradePhase
		}

		oldSet := newStatefulSetForPDScale()
		newSet := oldSet.DeepCopy()
		newSet.Spec.Replicas = int32Pointer(7)

		scaler, _, pvcIndexer, _, pvcControl := newFakeStoreScaler()

		pvc1 := newPVCForStatefulSet(oldSet, v1alpha1.StoreMemberType)
		pvc2 := pvc1.DeepCopy()
		pvc1.Name = ordinalPVCName(v1alpha1.StoreMemberType, oldSet.GetName(), *oldSet.Spec.Replicas)
		pvc2.Name = ordinalPVCName(v1alpha1.StoreMemberType, oldSet.GetName(), *oldSet.Spec.Replicas)
		if test.hasDeferAnn {
			pvc1.Annotations = map[string]string{}
			pvc1.Annotations[label.AnnPVCDeferDeleting] = time.Now().Format(time.RFC3339)
			pvc2.Annotations = map[string]string{}
			pvc2.Annotations[label.AnnPVCDeferDeleting] = time.Now().Format(time.RFC3339)
		}
		if test.hasPVC {
			pvcIndexer.Add(pvc1)
			pvcIndexer.Add(pvc2)
		}

		if test.pvcDeleteErr {
			pvcControl.SetDeletePVCError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err := scaler.ScaleOut(cc, oldSet, newSet)
		test.errExpectFn(g, err)
		if test.changed {
			g.Expect(int(*newSet.Spec.Replicas)).To(Equal(6))
		} else {
			g.Expect(int(*newSet.Spec.Replicas)).To(Equal(5))
		}
	}

	tests := []testcase{
		{
			name:           "normal",
			storeUpgrading: false,
			hasPVC:         true,
			hasDeferAnn:    false,
			pvcDeleteErr:   false,
			errExpectFn:    errExpectNil,
			changed:        true,
		},
		{
			name:           "store is upgrading",
			storeUpgrading: true,
			hasPVC:         true,
			hasDeferAnn:    false,
			pvcDeleteErr:   false,
			errExpectFn:    errExpectNil,
			changed:        false,
		},
		{
			name:           "cache don't have pvc",
			storeUpgrading: false,
			hasPVC:         false,
			hasDeferAnn:    false,
			pvcDeleteErr:   false,
			errExpectFn:    errExpectNil,
			changed:        true,
		},
		{
			name:           "pvc annotations defer deletion is not nil, pvc delete failed",
			storeUpgrading: false,
			hasPVC:         true,
			hasDeferAnn:    true,
			pvcDeleteErr:   true,
			errExpectFn:    errExpectNotNil,
			changed:        false,
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestStoreScalerScaleIn(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name           string
		storeUpgrading bool
		storeFun       func(cc *v1alpha1.CellCluster)
		delStoreErr    bool
		hasPVC         bool
		storeIDSynced  bool
		pvcUpdateErr   bool
		errExpectFn    func(*GomegaWithT, error)
		changed        bool
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)
		cc := newCellClusterForPD()
		test.storeFun(cc)

		if test.storeUpgrading {
			cc.Status.Store.Phase = v1alpha1.UpgradePhase
		}

		oldSet := newStatefulSetForPDScale()
		newSet := oldSet.DeepCopy()
		newSet.Spec.Replicas = int32Pointer(3)

		pod := &corev1.Pod{
			TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      storePodName(cc.GetName(), 4),
				Namespace: corev1.NamespaceDefault,
			},
		}

		scaler, pdControl, pvcIndexer, podIndexer, pvcControl := newFakeStoreScaler()

		if test.hasPVC {
			pvc := newPVCForStatefulSet(oldSet, v1alpha1.StoreMemberType)
			pvcIndexer.Add(pvc)
		}

		pod.Labels = map[string]string{}
		if test.storeIDSynced {
			pod.Labels[label.StoreIDLabelKey] = "1"
		}
		podIndexer.Add(pod)

		pdClient := controller.NewFakePDClient()
		pdControl.SetPDClient(cc, pdClient)

		if test.delStoreErr {
			pdClient.AddReaction(controller.DeleteStoreActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("delete store error")
			})
		}
		if test.pvcUpdateErr {
			pvcControl.SetUpdatePVCError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err := scaler.ScaleIn(cc, oldSet, newSet)
		test.errExpectFn(g, err)
		if test.changed {
			g.Expect(int(*newSet.Spec.Replicas)).To(Equal(4))
		} else {
			g.Expect(int(*newSet.Spec.Replicas)).To(Equal(5))
		}
	}

	tests := []testcase{
		{
			name:           "ordinal store is up, delete store success",
			storeUpgrading: false,
			storeFun:       normalStoreFun,
			delStoreErr:    false,
			hasPVC:         true,
			storeIDSynced:  true,
			pvcUpdateErr:   false,
			errExpectFn:    errExpectNotNil,
			changed:        false,
		},
		{
			name:           "store is upgrading",
			storeUpgrading: true,
			storeFun:       normalStoreFun,
			delStoreErr:    false,
			hasPVC:         true,
			storeIDSynced:  true,
			pvcUpdateErr:   false,
			errExpectFn:    errExpectNil,
			changed:        false,
		},
		{
			name:           "store id not match",
			storeUpgrading: false,
			storeFun:       normalStoreFun,
			delStoreErr:    false,
			hasPVC:         true,
			storeIDSynced:  false,
			pvcUpdateErr:   false,
			errExpectFn:    errExpectNotNil,
			changed:        false,
		},
		{
			name:           "status.Store.Stores is empty",
			storeUpgrading: false,
			storeFun: func(cc *v1alpha1.CellCluster) {
				normalStoreFun(cc)
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
			},
			delStoreErr:   false,
			hasPVC:        true,
			storeIDSynced: true,
			pvcUpdateErr:  false,
			errExpectFn:   errExpectNotNil,
			changed:       false,
		},
		{
			name:           "podName not match",
			storeUpgrading: false,
			storeFun: func(cc *v1alpha1.CellCluster) {
				normalStoreFun(cc)
				store := cc.Status.Store.Stores["1"]
				store.PodName = "xxx"
				cc.Status.Store.Stores["1"] = store
			},
			delStoreErr:   false,
			hasPVC:        true,
			storeIDSynced: true,
			pvcUpdateErr:  false,
			errExpectFn:   errExpectNotNil,
			changed:       false,
		},
		{
			name:           "store id is not integer",
			storeUpgrading: false,
			storeFun: func(cc *v1alpha1.CellCluster) {
				normalStoreFun(cc)
				store := cc.Status.Store.Stores["1"]
				store.ID = "not integer"
				cc.Status.Store.Stores["1"] = store
			},
			delStoreErr:   false,
			hasPVC:        true,
			storeIDSynced: true,
			pvcUpdateErr:  false,
			errExpectFn:   errExpectNotNil,
			changed:       false,
		},
		{
			name:           "store state is offline",
			storeUpgrading: false,
			storeFun: func(cc *v1alpha1.CellCluster) {
				normalStoreFun(cc)
				store := cc.Status.Store.Stores["1"]
				store.State = v1alpha1.StoreStateOffline
				cc.Status.Store.Stores["1"] = store
			},
			delStoreErr:   false,
			hasPVC:        true,
			storeIDSynced: true,
			pvcUpdateErr:  false,
			errExpectFn:   errExpectNotNil,
			changed:       false,
		},
		{
			name:           "store state is up, delete store success",
			storeUpgrading: false,
			storeFun:       normalStoreFun,
			delStoreErr:    false,
			hasPVC:         true,
			storeIDSynced:  true,
			pvcUpdateErr:   false,
			errExpectFn:    errExpectRequeue,
			changed:        false,
		},
		{
			name:           "store state is tombstone",
			storeUpgrading: false,
			storeFun:       tombstoneStoreFun,
			delStoreErr:    false,
			hasPVC:         true,
			storeIDSynced:  true,
			pvcUpdateErr:   false,
			errExpectFn:    errExpectNil,
			changed:        true,
		},
		{
			name:           "store state is tombstone, id is not integer",
			storeUpgrading: false,
			storeFun: func(cc *v1alpha1.CellCluster) {
				tombstoneStoreFun(cc)
				store := cc.Status.Store.TombstoneStores["1"]
				store.ID = "not integer"
				cc.Status.Store.TombstoneStores["1"] = store
			},
			delStoreErr:   false,
			hasPVC:        true,
			storeIDSynced: true,
			pvcUpdateErr:  false,
			errExpectFn:   errExpectNotNil,
			changed:       false,
		},
		{
			name:           "store state is tombstone, don't have pvc",
			storeUpgrading: false,
			storeFun:       tombstoneStoreFun,
			delStoreErr:    false,
			hasPVC:         false,
			storeIDSynced:  true,
			pvcUpdateErr:   false,
			errExpectFn:    errExpectNotNil,
			changed:        false,
		},
		{
			name:           "store state is tombstone, don't have pvc",
			storeUpgrading: false,
			storeFun:       tombstoneStoreFun,
			delStoreErr:    false,
			hasPVC:         true,
			storeIDSynced:  true,
			pvcUpdateErr:   true,
			errExpectFn:    errExpectNotNil,
			changed:        false,
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func newFakeStoreScaler() (*storeScaler, *controller.FakePDControl, cache.Indexer, cache.Indexer, *controller.FakePVCControl) {
	kubeCli := kubefake.NewSimpleClientset()

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeCli, 0)
	pvcInformer := kubeInformerFactory.Core().V1().PersistentVolumeClaims()
	podInformer := kubeInformerFactory.Core().V1().Pods()
	pdControl := controller.NewFakePDControl()
	pvcControl := controller.NewFakePVCControl(pvcInformer)

	return &storeScaler{generalScaler{pdControl, pvcInformer.Lister(), pvcControl}, podInformer.Lister()},
		pdControl, pvcInformer.Informer().GetIndexer(), podInformer.Informer().GetIndexer(), pvcControl
}

func normalStoreFun(cc *v1alpha1.CellCluster) {
	cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
		"1": {
			ID:      "1",
			PodName: ordinalPodName(v1alpha1.StoreMemberType, cc.GetName(), 4),
			State:   v1alpha1.StoreStateUp,
		},
	}
}

func tombstoneStoreFun(cc *v1alpha1.CellCluster) {
	cc.Status.Store.TombstoneStores = map[string]v1alpha1.KVStore{
		"1": {
			ID:      "1",
			PodName: ordinalPodName(v1alpha1.StoreMemberType, cc.GetName(), 4),
			State:   v1alpha1.StoreStateTombstone,
		},
	}
}

func errExpectRequeue(g *GomegaWithT, err error) {
	g.Expect(controller.IsRequeueError(err)).To(Equal(true))
}

func newPVCForStatefulSet(set *apps.StatefulSet, memberType v1alpha1.MemberType) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ordinalPVCName(memberType, set.GetName(), *set.Spec.Replicas-1),
			Namespace: metav1.NamespaceDefault,
		},
	}
}

func errExpectNotNil(g *GomegaWithT, err error) {
	g.Expect(err).To(HaveOccurred())
}
