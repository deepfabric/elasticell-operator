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

package v1alpha1

import (
	"testing"

	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPDIsAvailable(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name     string
		update   func(*CellCluster)
		expectFn func(*GomegaWithT, bool)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellCluster()
		test.update(cc)
		test.expectFn(g, cc.PDIsAvailable())
	}
	tests := []testcase{
		{
			name: "pd members count is 1",
			update: func(cc *CellCluster) {
				cc.Status.PD.Members = map[string]PDMember{
					"pd-0": {Name: "pd-0", Health: true},
				}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeFalse())
			},
		},
		{
			name: "pd members count is 2, but health count is 1",
			update: func(cc *CellCluster) {
				cc.Status.PD.Members = map[string]PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: false},
				}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeFalse())
			},
		},
		{
			name: "pd members count is 3, health count is 3, but ready replicas is 1",
			update: func(cc *CellCluster) {
				cc.Status.PD.Members = map[string]PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: true},
					"pd-2": {Name: "pd-2", Health: true},
				}
				cc.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 1}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeFalse())
			},
		},
		{
			name: "pd is available",
			update: func(cc *CellCluster) {
				cc.Status.PD.Members = map[string]PDMember{
					"pd-0": {Name: "pd-0", Health: true},
					"pd-1": {Name: "pd-1", Health: true},
					"pd-2": {Name: "pd-2", Health: true},
				}
				cc.Status.PD.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 3}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeTrue())
			},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestStoreIsAvailable(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name     string
		update   func(*CellCluster)
		expectFn func(*GomegaWithT, bool)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellCluster()
		test.update(cc)
		test.expectFn(g, cc.StoreIsAvailable())
	}
	tests := []testcase{
		{
			name: "stores count is 0",
			update: func(cc *CellCluster) {
				cc.Status.Store.Stores = map[string]KVStore{}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeFalse())
			},
		},
		{
			name: "stores count is 1, but available count is 0",
			update: func(cc *CellCluster) {
				cc.Status.Store.Stores = map[string]KVStore{
					"cell-store-0": {PodName: "cell-store-0", State: StoreStateDown},
				}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeFalse())
			},
		},
		{
			name: "cell-store stores count is 1, available count is 1, ready replicas is 0",
			update: func(cc *CellCluster) {
				cc.Status.Store.Stores = map[string]KVStore{
					"cell-store-0": {PodName: "cell-store-0", State: StoreStateUp},
				}
				cc.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 0}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeFalse())
			},
		},
		{
			name: "cell-store is available",
			update: func(cc *CellCluster) {
				cc.Status.Store.Stores = map[string]KVStore{
					"cell-store-0": {PodName: "cell-store-0", State: StoreStateUp},
				}
				cc.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 1}
			},
			expectFn: func(g *GomegaWithT, b bool) {
				g.Expect(b).To(BeTrue())
			},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func newCellCluster() *CellCluster {
	return &CellCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CellCluster",
			APIVersion: "deepfabric.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pd",
			Namespace: corev1.NamespaceDefault,
			UID:       types.UID("test"),
		},
		Spec: CellClusterSpec{
			PD: PDSpec{
				Replicas: 3,
			},
			Store: StoreSpec{
				Replicas: 3,
			},
			Proxy: ProxySpec{
				Replicas: 1,
			},
		},
	}
}
