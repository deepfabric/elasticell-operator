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

package discovery

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDiscoveryDiscovery(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name           string
		ns             string
		url            string
		currentCluster *clusterInfo
		ccFn           func() (*v1alpha1.CellCluster, error)
		getMembersFn   func() (*controller.MembersInfo, error)
		expectFn       func(*GomegaWithT, *cellDiscovery, string, error)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		fakePDControl := controller.NewFakePDControl()
		pdClient := controller.NewFakePDClient()
		cc, err := test.ccFn()
		if err == nil {
			fakePDControl.SetPDClient(cc, pdClient)
		}
		/*
			pdClient.AddReaction(controller.GetMembersActionType, func(action *controller.Action) (interface{}, error) {
				return test.getMembersFn()
			})
		*/
		cd := &cellDiscovery{
			pdControl: fakePDControl,
			ccGetFn: func(ns, ccName string) (*v1alpha1.CellCluster, error) {
				return cc, err
			},
			ccUpdateFn: func(ns string, cell *v1alpha1.CellCluster) (*v1alpha1.CellCluster, error) {
				cc.Status.PdPeerURL = cell.Status.PdPeerURL
				return cc, err
			},
			currentCluster: test.currentCluster,
		}
		os.Setenv("MY_POD_NAMESPACE", test.ns)
		re, err := cd.Discover(test.url)
		test.expectFn(g, cd, re, err)
	}
	tests := []testcase{
		{
			name:           "advertisePeerURL is empty",
			ns:             "default",
			url:            "",
			currentCluster: &clusterInfo{},
			ccFn:           newCC,
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "advertisePeerURL is empty")).To(BeTrue())
			},
		},
		{
			name:           "advertisePeerURL is wrong",
			ns:             "default",
			url:            "demo-pd-0.demo-pd.default:2380",
			currentCluster: &clusterInfo{},
			ccFn:           newCC,
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "advertisePeerURL format is wrong: ")).To(BeTrue())
			},
		},
		{
			name:           "namespace is wrong",
			ns:             "default1",
			url:            "demo-pd-0.demo-pd.default.svc:2380",
			currentCluster: &clusterInfo{},
			ccFn:           newCC,
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "is not equal to discovery namespace:")).To(BeTrue())
			},
		},
		{
			name:           "failed to get cellcluster",
			ns:             "default",
			url:            "demo-pd-0.demo-pd.default.svc:2380",
			currentCluster: &clusterInfo{},
			ccFn: func() (*v1alpha1.CellCluster, error) {
				return nil, fmt.Errorf("failed to get cellcluster")
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "failed to get cellcluster")).To(BeTrue())
			},
		},
		/*
			{
				name:           "failed to get members",
				ns:             "default",
				url:            "demo-pd-0.demo-pd.default.svc:2380",
				currentCluster: &clusterInfo{},
				ccFn:           newCC,
				getMembersFn: func() (*controller.MembersInfo, error) {
					return nil, fmt.Errorf("get members failed")
				},
				expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
					g.Expect(err).To(HaveOccurred())
					g.Expect(strings.Contains(err.Error(), "get members failed")).To(BeTrue())
					g.Expect(len(cd.currentCluster.peers)).To(Equal(1))
					g.Expect(cd.currentCluster.peers["demo-pd-0"]).To(Equal(""))
				},
			},
		*/
		{
			name: "resourceVersion changed",
			ns:   "default",
			url:  "demo-pd-0.demo-pd.default.svc:2380",
			ccFn: newCC,
			currentCluster: &clusterInfo{
				resourceVersion: "2",
				peers: map[string]string{
					"demo-pd-0": "",
					"demo-pd-1": "",
				},
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				// g.Expect(strings.Contains(err.Error(), "getMembers failed")).To(BeTrue())
				g.Expect(len(cd.currentCluster.peers)).To(Equal(1))
				g.Expect(cd.currentCluster.peers["demo-pd-0"]).To(Equal("demo-pd-0.demo-pd.default.svc:2380"))
			},
		},
		{
			name:           "1 cluster, first ordinal",
			ns:             "default",
			url:            "demo-pd-0.demo-pd.default.svc:2380",
			currentCluster: &clusterInfo{},
			ccFn:           newCC,
			getMembersFn: func() (*controller.MembersInfo, error) {
				return nil, fmt.Errorf("there are no pd members")
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "please wait until all pd pod startup")).To(BeTrue())
				g.Expect(len(cd.currentCluster.peers)).To(Equal(1))
				g.Expect(cd.currentCluster.peers["demo-pd-0"]).To(Equal("demo-pd-0.demo-pd.default.svc:2380"))
			},
		},
		{
			name: "1 cluster, second ordinal",
			ns:   "default",
			url:  "demo-pd-1.demo-pd.default.svc:2380",
			ccFn: newCC,
			getMembersFn: func() (*controller.MembersInfo, error) {
				return nil, fmt.Errorf("there are no pd members 2")
			},
			currentCluster: &clusterInfo{
				resourceVersion: "1",
				peers: map[string]string{
					"demo-pd-0": "demo-pd-0.demo-pd.default.svc:2380",
				},
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "please wait until all pd pod startup")).To(BeTrue())
				g.Expect(len(cd.currentCluster.peers)).To(Equal(2))
				g.Expect(cd.currentCluster.peers["demo-pd-0"]).To(Equal("demo-pd-0.demo-pd.default.svc:2380"))
				g.Expect(cd.currentCluster.peers["demo-pd-1"]).To(Equal("demo-pd-1.demo-pd.default.svc:2380"))
			},
		},
		{
			name: "1 cluster, third ordinal, return the initial-cluster args",
			ns:   "default",
			url:  "demo-pd-2.demo-pd.default.svc:2380",
			ccFn: newCC,
			currentCluster: &clusterInfo{
				resourceVersion: "1",
				peers: map[string]string{
					"demo-pd-0": "demo-pd-0.demo-pd.default.svc:2380",
					"demo-pd-1": "demo-pd-1.demo-pd.default.svc:2380",
				},
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(cd.currentCluster.peers)).To(Equal(3))
				g.Expect(cd.currentCluster.peers["demo-pd-0"]).To(Equal("demo-pd-0.demo-pd.default.svc:2380"))
				g.Expect(cd.currentCluster.peers["demo-pd-1"]).To(Equal("demo-pd-1.demo-pd.default.svc:2380"))
				g.Expect(cd.currentCluster.peers["demo-pd-2"]).To(Equal("demo-pd-2.demo-pd.default.svc:2380"))
				// g.Expect(s).To(Equal("demo-pd-0=demo-pd-0.demo-pd.default.svc:2380,demo-pd-1=demo-pd-1.demo-pd.default.svc:2380,demo-pd-2=demo-pd-2.demo-pd.default.svc:2380"))
			},
		},
		{
			name: "1 cluster, the first ordinal third request",
			ns:   "default",
			url:  "demo-pd-0.demo-pd.default.svc:2380",
			ccFn: newCC,
			getMembersFn: func() (*controller.MembersInfo, error) {
				return &controller.MembersInfo{
					Members: []*controller.PdMember{
						{
							PeerUrls: []string{"demo-pd-2.demo-pd.default.svc:2380"},
						},
					},
				}, nil
			},
			currentCluster: &clusterInfo{
				resourceVersion: "1",
				peers: map[string]string{
					"demo-pd-1": "",
					"demo-pd-2": "",
				},
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(cd.currentCluster.peers)).To(Equal(3))
				g.Expect(cd.currentCluster.peers["demo-pd-1"]).To(Equal(""))
				// g.Expect(s).To(Equal("demo-pd-0=demo-pd-0.demo-pd.default.svc:2380,demo-pd-1=,demo-pd-2="))
			},
		},
		{
			name: "1 cluster, the second ordinal second request",
			ns:   "default",
			url:  "demo-pd-1.demo-pd.default.svc:2380",
			ccFn: newCCPeerURL,
			getMembersFn: func() (*controller.MembersInfo, error) {
				return &controller.MembersInfo{
					Members: []*controller.PdMember{
						{
							PeerUrls: []string{"demo-pd-0.demo-pd.default.svc:2380"},
						},
						{
							PeerUrls: []string{"demo-pd-2.demo-pd.default.svc:2380"},
						},
					},
				}, nil
			},
			currentCluster: &clusterInfo{
				resourceVersion: "1",
				peers:           map[string]string{},
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(len(cd.currentCluster.peers)).To(Equal(1))
				g.Expect(s).To(Equal("demo-pd-0=demo-pd-0.demo-pd.default.svc:2380,demo-pd-1=demo-pd-1.demo-pd.default.svc:2380,demo-pd-2=demo-pd-2.demo-pd.default.svc:2380"))
			},
		},
		/*
			{
				name: "1 cluster, the fourth ordinal request, get members success",
				ns:   "default",
				url:  "demo-pd-3.demo-pd.default.svc:2380",
				ccFn: func() (*v1alpha1.CellCluster, error) {
					cc, _ := newCC()
					cc.Spec.PD.Replicas = 5
					return cc, nil
				},
				getMembersFn: func() (*controller.MembersInfo, error) {
					return &controller.MembersInfo{
						Members: []*controller.PdMember{
							{
								PeerUrls: []string{"demo-pd-0.demo-pd.default.svc:2380"},
							},
							{
								PeerUrls: []string{"demo-pd-1.demo-pd.default.svc:2380"},
							},
							{
								PeerUrls: []string{"demo-pd-2.demo-pd.default.svc:2380"},
							},
						},
					}, nil
				},
				currentCluster: &clusterInfo{
					resourceVersion: "1",
					peers:           map[string]string{},
				},
				expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(len(cd.currentCluster.peers)).To(Equal(0))
					g.Expect(s).To(Equal("--join=demo-pd-0.demo-pd.default.svc:2380,demo-pd-1.demo-pd.default.svc:2380,demo-pd-2.demo-pd.default.svc:2380"))
				},
			},
		*/
	}
	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestDiscoveryGetProxyConfig(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name     string
		ns       string
		ccFn     func() (*v1alpha1.CellCluster, error)
		expectFn func(*GomegaWithT, *cellDiscovery, string, error)
	}
	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		fakePDControl := controller.NewFakePDControl()
		pdClient := controller.NewFakePDClient()
		cc, err := test.ccFn()
		if err == nil {
			fakePDControl.SetPDClient(cc, pdClient)
		}
		cd := &cellDiscovery{
			pdControl: fakePDControl,
			ccGetFn: func(ns, ccName string) (*v1alpha1.CellCluster, error) {
				return cc, err
			},
			ccUpdateFn: func(ns string, cell *v1alpha1.CellCluster) (*v1alpha1.CellCluster, error) {
				cc.Status.PdPeerURL = cell.Status.PdPeerURL
				return cc, err
			},
		}
		os.Setenv("MY_POD_NAMESPACE", test.ns)
		re, err := cd.GetProxyConfig()
		test.expectFn(g, cd, re, err)
	}
	tests := []testcase{

		{
			name: "failed to get cellcluster",
			ns:   "default",
			ccFn: func() (*v1alpha1.CellCluster, error) {
				return nil, fmt.Errorf("failed to get cellcluster")
			},
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "failed to get cellcluster")).To(BeTrue())
			},
		},

		{
			name: "cell cluster status PdPeerURL is null",
			ns:   "default",
			ccFn: newCC,
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "cell cluster status PdPeerURL is null")).To(BeTrue())
			},
		},

		{
			name: "get pd config success",
			ns:   "default",
			ccFn: newCCPeerURL,
			expectFn: func(g *GomegaWithT, cd *cellDiscovery, s string, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(strings.Contains(s, "demo-pd-0.demo-pd.default.svc:2380")).To(BeTrue())
			},
		},
	}
	for i := range tests {
		testFn(&tests[i], t)
	}
}

func newCC() (*v1alpha1.CellCluster, error) {
	return &v1alpha1.CellCluster{
		TypeMeta: metav1.TypeMeta{Kind: "CellCluster", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "demo",
			Namespace:       metav1.NamespaceDefault,
			ResourceVersion: "1",
		},
		Spec: v1alpha1.CellClusterSpec{
			PD: v1alpha1.PDSpec{Replicas: 3},
		},
		Status: v1alpha1.CellClusterStatus{
			PdPeerURL: "",
		},
	}, nil
}

func newCCPeerURL() (*v1alpha1.CellCluster, error) {
	return &v1alpha1.CellCluster{
		TypeMeta: metav1.TypeMeta{Kind: "CellCluster", APIVersion: "v1alpha1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "demo",
			Namespace:       metav1.NamespaceDefault,
			ResourceVersion: "1",
		},
		Spec: v1alpha1.CellClusterSpec{
			PD: v1alpha1.PDSpec{Replicas: 3},
		},
		Status: v1alpha1.CellClusterStatus{
			PdPeerURL: "demo-pd-0=demo-pd-0.demo-pd.default.svc:2380,demo-pd-1=demo-pd-1.demo-pd.default.svc:2380,demo-pd-2=demo-pd-2.demo-pd.default.svc:2380",
		},
	}, nil
}
