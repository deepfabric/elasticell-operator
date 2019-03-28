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
	"fmt"
	"testing"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRequeueError(t *testing.T) {
	g := NewGomegaWithT(t)

	err := RequeueErrorf("i am a requeue %s", "error")
	g.Expect(IsRequeueError(err)).To(BeTrue())
	_, ok := err.(error)
	g.Expect(ok).To(BeTrue())
	g.Expect(err.Error()).To(Equal("i am a requeue error"))
	g.Expect(IsRequeueError(fmt.Errorf("i am not a requeue error"))).To(BeFalse())
}

func TestGetOwnerRef(t *testing.T) {
	g := NewGomegaWithT(t)

	cc := newCellCluster()
	cc.UID = types.UID("demo-uid")
	ref := GetOwnerRef(cc)
	g.Expect(ref.APIVersion).To(Equal(controllerKind.GroupVersion().String()))
	g.Expect(ref.Kind).To(Equal(controllerKind.Kind))
	g.Expect(ref.Name).To(Equal(cc.GetName()))
	g.Expect(ref.UID).To(Equal(types.UID("demo-uid")))
	g.Expect(*ref.Controller).To(BeTrue())
	g.Expect(*ref.BlockOwnerDeletion).To(BeTrue())
}

func TestGetServiceType(t *testing.T) {
	g := NewGomegaWithT(t)

	services := []v1alpha1.Service{
		{
			Name: "a",
			Type: string(corev1.ServiceTypeNodePort),
		},
		{
			Name: "b",
			Type: string(corev1.ServiceTypeLoadBalancer),
		},
		{
			Name: "c",
			Type: "Other",
		},
	}

	g.Expect(GetServiceType(services, "a")).To(Equal(corev1.ServiceTypeNodePort))
	g.Expect(GetServiceType(services, "b")).To(Equal(corev1.ServiceTypeLoadBalancer))
	g.Expect(GetServiceType(services, "c")).To(Equal(corev1.ServiceTypeClusterIP))
	g.Expect(GetServiceType(services, "d")).To(Equal(corev1.ServiceTypeClusterIP))
}

func TestStoreCapacity(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name     string
		limit    *v1alpha1.ResourceRequirement
		expectFn func(*GomegaWithT, string)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)
		test.expectFn(g, StoreCapacity(test.limit))
	}
	tests := []testcase{
		{
			name:  "limit is nil",
			limit: nil,
			expectFn: func(g *GomegaWithT, s string) {
				g.Expect(s).To(Equal("0"))
			},
		},
		{
			name: "storage is empty",
			limit: &v1alpha1.ResourceRequirement{
				Storage: "",
			},
			expectFn: func(g *GomegaWithT, s string) {
				g.Expect(s).To(Equal("0"))
			},
		},
		{
			name: "failed to parse quantity",
			limit: &v1alpha1.ResourceRequirement{
				Storage: "100x",
			},
			expectFn: func(g *GomegaWithT, s string) {
				g.Expect(s).To(Equal("0"))
			},
		},
		{
			name: "100Gi",
			limit: &v1alpha1.ResourceRequirement{
				Storage: "100Gi",
			},
			expectFn: func(g *GomegaWithT, s string) {
				g.Expect(s).To(Equal("100GB"))
			},
		},
		{
			name: "100GiB",
			limit: &v1alpha1.ResourceRequirement{
				Storage: "100Gi",
			},
			expectFn: func(g *GomegaWithT, s string) {
				g.Expect(s).To(Equal("100GB"))
			},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestPDMemberName(t *testing.T) {
	g := NewGomegaWithT(t)
	g.Expect(PDMemberName("demo")).To(Equal("demo-pd"))
}

func TestPDPeerMemberName(t *testing.T) {
	g := NewGomegaWithT(t)
	g.Expect(PDPeerMemberName("demo")).To(Equal("demo-pd-peer"))
}

func TestStoreMemberName(t *testing.T) {
	g := NewGomegaWithT(t)
	g.Expect(StoreMemberName("demo")).To(Equal("demo-store"))
}

func TestProxyMemberName(t *testing.T) {
	g := NewGomegaWithT(t)
	g.Expect(ProxyMemberName("demo")).To(Equal("demo-proxy"))
}

func TestSetIfNotEmpty(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name     string
		key      string
		value    string
		expectFn func(*GomegaWithT, map[string]string)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		m := map[string]string{"a": "a"}
		setIfNotEmpty(m, test.key, test.value)

		test.expectFn(g, m)
	}
	tests := []testcase{
		{
			name:  "has key",
			key:   "a",
			value: "aa",
			expectFn: func(g *GomegaWithT, m map[string]string) {
				g.Expect(m["a"]).To(Equal("aa"))
			},
		},
		{
			name:  "don't have key",
			key:   "b",
			value: "b",
			expectFn: func(g *GomegaWithT, m map[string]string) {
				g.Expect(m["b"]).To(Equal("b"))
			},
		},
		{
			name:  "new key's value is empty",
			key:   "b",
			value: "",
			expectFn: func(g *GomegaWithT, m map[string]string) {
				g.Expect(m["b"]).To(Equal(""))
			},
		},
		{
			name:  "old key's value is empty",
			key:   "a",
			value: "",
			expectFn: func(g *GomegaWithT, m map[string]string) {
				g.Expect(m["a"]).To(Equal("a"))
			},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func collectEvents(source <-chan string) []string {
	done := false
	events := make([]string, 0)
	for !done {
		select {
		case event := <-source:
			events = append(events, event)
		default:
			done = true
		}
	}
	return events
}

func newCellCluster() *v1alpha1.CellCluster {
	cc := &v1alpha1.CellCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: metav1.NamespaceDefault,
		},
	}
	return cc
}

func newService(cc *v1alpha1.CellCluster, _ string) *corev1.Service {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetName(cc.Name, "pd"),
			Namespace: metav1.NamespaceDefault,
		},
	}
	return svc
}

func newStatefulSet(cc *v1alpha1.CellCluster, _ string) *apps.StatefulSet {
	set := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetName(cc.Name, "pd"),
			Namespace: metav1.NamespaceDefault,
		},
	}
	return set
}

// GetName concatenate cell cluster name and member name, used for controller managed resource name
func GetName(ccName string, name string) string {
	return fmt.Sprintf("%s-%s", ccName, name)
}
