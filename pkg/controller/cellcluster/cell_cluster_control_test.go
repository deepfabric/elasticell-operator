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

package cellcluster

import (
	"fmt"
	"strings"
	"testing"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned/fake"
	informers "github.com/deepfabric/elasticell-operator/pkg/client/informers/externalversions"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	mm "github.com/deepfabric/elasticell-operator/pkg/manager/member"
	"github.com/deepfabric/elasticell-operator/pkg/manager/meta"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
)

func TestCellClusterControlUpdateCellCluster(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name                      string
		update                    func(cluster *v1alpha1.CellCluster)
		syncReclaimPolicyErr      bool
		syncPDMemberManagerErr    bool
		syncStoreMemberManagerErr bool
		syncProxyMemberManagerErr bool
		syncMetaManagerErr        bool
		errExpectFn               func(*GomegaWithT, error)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellClusterForCellClusterControl()
		if test.update != nil {
			test.update(cc)
		}
		control, reclaimPolicyManager, pdMemberManager, storeMemberManager, proxyMemberManager, metaManager := newFakeCellClusterControl()

		if test.syncReclaimPolicyErr {
			reclaimPolicyManager.SetSyncError(fmt.Errorf("reclaim policy sync error"))
		}
		if test.syncPDMemberManagerErr {
			pdMemberManager.SetSyncError(fmt.Errorf("pd member manager sync error"))
		}
		if test.syncStoreMemberManagerErr {
			storeMemberManager.SetSyncError(fmt.Errorf("store member manager sync error"))
		}
		if test.syncProxyMemberManagerErr {
			proxyMemberManager.SetSyncError(fmt.Errorf("proxy member manager sync error"))
		}
		if test.syncMetaManagerErr {
			metaManager.SetSyncError(fmt.Errorf("meta manager sync error"))
		}

		err := control.UpdateCellCluster(cc)
		if test.errExpectFn != nil {
			test.errExpectFn(g, err)
		}
	}
	tests := []testcase{
		{
			name:                      "reclaim policy sync error",
			update:                    nil,
			syncReclaimPolicyErr:      true,
			syncPDMemberManagerErr:    false,
			syncStoreMemberManagerErr: false,
			syncProxyMemberManagerErr: false,
			syncMetaManagerErr:        false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "reclaim policy sync error")).To(Equal(true))
			},
		},
		{
			name:                      "pd member manager sync error",
			update:                    nil,
			syncReclaimPolicyErr:      false,
			syncPDMemberManagerErr:    true,
			syncStoreMemberManagerErr: false,
			syncProxyMemberManagerErr: false,
			syncMetaManagerErr:        false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "pd member manager sync error")).To(Equal(true))
			},
		},
		{
			name: "store member manager sync error",
			update: func(cluster *v1alpha1.CellCluster) {
				cluster.Status.PD.Members = map[string]v1alpha1.PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: true},
					"pd-2": {Name: "pd-2", Health: true},
				}
				cluster.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
			},
			syncReclaimPolicyErr:      false,
			syncPDMemberManagerErr:    false,
			syncStoreMemberManagerErr: true,
			syncProxyMemberManagerErr: false,
			syncMetaManagerErr:        false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "store member manager sync error")).To(Equal(true))
			},
		},
		{
			name: "proxy member manager sync error",
			update: func(cluster *v1alpha1.CellCluster) {
				cluster.Status.PD.Members = map[string]v1alpha1.PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: true},
					"pd-2": {Name: "pd-2", Health: true},
				}
				cluster.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
				cluster.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"store-0": {PodName: "store-0", State: v1alpha1.StoreStateUp},
					"store-1": {PodName: "store-1", State: v1alpha1.StoreStateUp},
					"store-2": {PodName: "store-2", State: v1alpha1.StoreStateUp},
				}
				cluster.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
			},
			syncReclaimPolicyErr:      false,
			syncPDMemberManagerErr:    false,
			syncStoreMemberManagerErr: false,
			syncProxyMemberManagerErr: true,
			syncMetaManagerErr:        false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "proxy member manager sync error")).To(Equal(true))
			},
		},
		{
			name: "meta manager sync error",
			update: func(cluster *v1alpha1.CellCluster) {
				cluster.Status.PD.Members = map[string]v1alpha1.PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: true},
					"pd-2": {Name: "pd-2", Health: true},
				}
				cluster.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
				cluster.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"store-0": {PodName: "store-0", State: v1alpha1.StoreStateUp},
					"store-1": {PodName: "store-1", State: v1alpha1.StoreStateUp},
					"store-2": {PodName: "store-2", State: v1alpha1.StoreStateUp},
				}
				cluster.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
			},
			syncReclaimPolicyErr:      false,
			syncPDMemberManagerErr:    false,
			syncStoreMemberManagerErr: false,
			syncProxyMemberManagerErr: false,
			syncMetaManagerErr:        true,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "meta manager sync error")).To(Equal(true))
			},
		},
		{
			name: "normal",
			update: func(cluster *v1alpha1.CellCluster) {
				cluster.Status.PD.Members = map[string]v1alpha1.PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: true},
					"pd-2": {Name: "pd-2", Health: true},
				}
				cluster.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
				cluster.Status.Store.Stores = map[string]v1alpha1.KVStore{
					"store-0": {PodName: "store-0", State: v1alpha1.StoreStateUp},
					"store-1": {PodName: "store-1", State: v1alpha1.StoreStateUp},
					"store-2": {PodName: "store-2", State: v1alpha1.StoreStateUp},
				}
				cluster.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
			},
			syncReclaimPolicyErr:      false,
			syncPDMemberManagerErr:    false,
			syncStoreMemberManagerErr: false,
			syncProxyMemberManagerErr: false,
			syncMetaManagerErr:        false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestCellClusterStatusEquality(t *testing.T) {
	g := NewGomegaWithT(t)
	tcStatus := v1alpha1.CellClusterStatus{}

	tcStatusCopy := tcStatus.DeepCopy()
	tcStatusCopy.PD = v1alpha1.PDStatus{}
	g.Expect(apiequality.Semantic.DeepEqual(&tcStatus, tcStatusCopy)).To(Equal(true))

	tcStatusCopy = tcStatus.DeepCopy()
	tcStatusCopy.PD.Phase = v1alpha1.NormalPhase
	g.Expect(apiequality.Semantic.DeepEqual(&tcStatus, tcStatusCopy)).To(Equal(false))
}

func newFakeCellClusterControl() (ControlInterface, *meta.FakeReclaimPolicyManager, *mm.FakePDMemberManager, *mm.FakeStoreMemberManager, *mm.FakeProxyMemberManager, *meta.FakeMetaManager) {
	cli := fake.NewSimpleClientset()
	tcInformer := informers.NewSharedInformerFactory(cli, 0).Deepfabric().V1alpha1().CellClusters()
	recorder := record.NewFakeRecorder(10)

	ccControl := controller.NewFakeCellClusterControl(tcInformer)
	pdMemberManager := mm.NewFakePDMemberManager()
	storeMemberManager := mm.NewFakeStoreMemberManager()
	proxyMemberManager := mm.NewFakeProxyMemberManager()
	reclaimPolicyManager := meta.NewFakeReclaimPolicyManager()
	metaManager := meta.NewFakeMetaManager()
	opc := mm.NewFakeOrphanPodsCleaner()
	control := NewDefaultCellClusterControl(ccControl, pdMemberManager, storeMemberManager, proxyMemberManager, reclaimPolicyManager, metaManager, opc, recorder)

	return control, reclaimPolicyManager, pdMemberManager, storeMemberManager, proxyMemberManager, metaManager
}

func newCellClusterForCellClusterControl() *v1alpha1.CellCluster {
	return &v1alpha1.CellCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CellCluster",
			APIVersion: "deepfabric.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pd",
			Namespace: corev1.NamespaceDefault,
			UID:       types.UID("test"),
		},
		Spec: v1alpha1.CellClusterSpec{
			PD: v1alpha1.PDSpec{
				Replicas: 3,
			},
			Store: v1alpha1.StoreSpec{
				Replicas: 3,
			},
			Proxy: v1alpha1.ProxySpec{
				Replicas: 1,
			},
		},
	}
}
