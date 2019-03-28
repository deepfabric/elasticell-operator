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

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned/fake"
	informers "github.com/deepfabric/elasticell-operator/pkg/client/informers/externalversions"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestProxyMemberManagerSyncCreate(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                     string
		prepare                  func(cluster *v1alpha1.CellCluster)
		errWhenCreateStatefulSet bool
		err                      bool
		setCreated               bool
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellClusterForProxy()
		cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
			"store-0": {PodName: "store-0", State: v1alpha1.StoreStateUp},
		}
		cc.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 1}

		ns := cc.GetNamespace()
		ccName := cc.GetName()
		oldSpec := cc.Spec
		if test.prepare != nil {
			test.prepare(cc)
		}

		pxmm, fakeSetControl, _, _ := newFakeProxyMemberManager()

		if test.errWhenCreateStatefulSet {
			fakeSetControl.SetCreateStatefulSetError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err := pxmm.Sync(cc)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}

		g.Expect(cc.Spec).To(Equal(oldSpec))

		tc1, err := pxmm.setLister.StatefulSets(ns).Get(controller.ProxyMemberName(ccName))
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
		},
		{
			name: "store is not available",
			prepare: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Stores = map[string]v1alpha1.KVStore{}
			},
			errWhenCreateStatefulSet: false,
			err:                      true,
			setCreated:               false,
		},
		{
			name:                     "error when create statefulset",
			prepare:                  nil,
			errWhenCreateStatefulSet: true,
			err:                      true,
			setCreated:               false,
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestProxyMemberManagerSyncUpdate(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                     string
		modify                   func(cluster *v1alpha1.CellCluster)
		errWhenUpdateStatefulSet bool
		statusChange             func(*apps.StatefulSet)
		err                      bool
		expectStatefulSetFn      func(*GomegaWithT, *apps.StatefulSet, error)
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)

		cc := newCellClusterForProxy()
		cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
			"store-0": {PodName: "store-0", State: v1alpha1.StoreStateUp},
		}
		cc.Status.Store.StatefulSet = &apps.StatefulSetStatus{ReadyReplicas: 1}

		ns := cc.GetNamespace()
		ccName := cc.GetName()

		pxmm, fakeSetControl, _, _ := newFakeProxyMemberManager()

		if test.statusChange == nil {
			fakeSetControl.SetStatusChange(func(set *apps.StatefulSet) {
				set.Status.Replicas = *set.Spec.Replicas
				set.Status.CurrentRevision = "proxy-1"
				set.Status.UpdateRevision = "proxy-1"
				observedGeneration := int64(1)
				set.Status.ObservedGeneration = &observedGeneration
			})
		} else {
			fakeSetControl.SetStatusChange(test.statusChange)
		}

		err := pxmm.Sync(cc)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(err).NotTo(HaveOccurred())
		_, err = pxmm.setLister.StatefulSets(ns).Get(controller.ProxyMemberName(ccName))
		g.Expect(err).NotTo(HaveOccurred())

		tc1 := cc.DeepCopy()
		test.modify(tc1)

		if test.errWhenUpdateStatefulSet {
			fakeSetControl.SetUpdateStatefulSetError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err = pxmm.Sync(tc1)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}

		if test.expectStatefulSetFn != nil {
			set, err := pxmm.setLister.StatefulSets(ns).Get(controller.ProxyMemberName(ccName))
			test.expectStatefulSetFn(g, set, err)
		}
	}

	tests := []testcase{
		{
			name: "normal",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Proxy.Replicas = 5
				cc.Status.PD.Phase = v1alpha1.NormalPhase
				cc.Status.Proxy.Phase = v1alpha1.NormalPhase
			},
			errWhenUpdateStatefulSet: false,
			err:                      false,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(int(*set.Spec.Replicas)).To(Equal(5))
			},
		},
		{
			name: "error when update statefulset",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Proxy.Replicas = 5
				cc.Status.PD.Phase = v1alpha1.NormalPhase
				cc.Status.Proxy.Phase = v1alpha1.NormalPhase
			},
			errWhenUpdateStatefulSet: true,
			err:                      true,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestProxyMemberManagerProxyStatefulSetIsUpgrading(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name            string
		setUpdate       func(*apps.StatefulSet)
		hasPod          bool
		updatePod       func(*corev1.Pod)
		errExpectFn     func(*GomegaWithT, error)
		expectUpgrading bool
	}
	testFn := func(test *testcase, t *testing.T) {
		pdmm, _, podIndexer, _ := newFakeProxyMemberManager()
		cc := newCellClusterForProxy()
		cc.Status.Store.Stores = map[string]v1alpha1.KVStore{
			"store-0": {PodName: "store-0", State: v1alpha1.StoreStateUp},
		}
		cc.Status.Proxy.StatefulSet = &apps.StatefulSetStatus{
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
					Name:        ordinalPodName(v1alpha1.ProxyMemberType, cc.GetName(), 0),
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{},
					Labels:      label.New().Instance(cc.GetLabels()[label.InstanceLabelKey]).Proxy().Labels(),
				},
			}
			if test.updatePod != nil {
				test.updatePod(pod)
			}
			podIndexer.Add(pod)
		}
		b, err := pdmm.proxyStatefulSetIsUpgradingFn(pdmm.podLister, set, cc)
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
			hasPod:          false,
			updatePod:       nil,
			errExpectFn:     nil,
			expectUpgrading: true,
		},
		{
			name:            "pod don't have revision hash",
			setUpdate:       nil,
			hasPod:          true,
			updatePod:       nil,
			errExpectFn:     nil,
			expectUpgrading: false,
		},
		{
			name:      "pod have revision hash, not equal statefulset's",
			setUpdate: nil,
			hasPod:    true,
			updatePod: func(pod *corev1.Pod) {
				pod.Labels[apps.ControllerRevisionHashLabelKey] = "v2"
			},
			errExpectFn:     nil,
			expectUpgrading: true,
		},
		{
			name:      "pod have revision hash, equal statefulset's",
			setUpdate: nil,
			hasPod:    true,
			updatePod: func(pod *corev1.Pod) {
				pod.Labels[apps.ControllerRevisionHashLabelKey] = "v3"
			},
			errExpectFn:     nil,
			expectUpgrading: false,
		},
	}

	for i := range tests {
		t.Logf(tests[i].name)
		testFn(&tests[i], t)
	}
}

func TestProxyMemberManagerSyncCellClusterStatus(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name        string
		updateCC    func(*v1alpha1.CellCluster)
		upgradingFn func(corelisters.PodLister, *apps.StatefulSet, *v1alpha1.CellCluster) (bool, error)
		errExpectFn func(*GomegaWithT, error)
		ccExpectFn  func(*GomegaWithT, *v1alpha1.CellCluster)
	}
	status := apps.StatefulSetStatus{
		Replicas: int32(3),
	}
	// now := metav1.Time{Time: time.Now()}
	testFn := func(test *testcase, t *testing.T) {
		cc := newCellClusterForPD()
		cc.Status.PD.Phase = v1alpha1.NormalPhase
		cc.Status.Proxy.Phase = v1alpha1.NormalPhase
		set := &apps.StatefulSet{
			Status: status,
		}
		if test.updateCC != nil {
			test.updateCC(cc)
		}
		pdmm, _, _, _ := newFakeProxyMemberManager()

		if test.upgradingFn != nil {
			pdmm.proxyStatefulSetIsUpgradingFn = test.upgradingFn
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
			upgradingFn: func(lister corelisters.PodLister, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, fmt.Errorf("whether upgrading failed")
			},
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "whether upgrading failed")).To(BeTrue())
			},
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Proxy.StatefulSet.Replicas).To(Equal(int32(3)))
			},
		},
		{
			name:     "statefulset is upgrading",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return true, nil
			},
			errExpectFn: nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Proxy.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Proxy.Phase).To(Equal(v1alpha1.UpgradePhase))
			},
		},
		{
			name: "statefulset is upgrading but pd is upgrading",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.UpgradePhase
			},
			upgradingFn: func(lister corelisters.PodLister, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return true, nil
			},
			errExpectFn: nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Proxy.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Proxy.Phase).To(Equal(v1alpha1.NormalPhase))
			},
		},
		{
			name: "statefulset is upgrading but store is upgrading",
			updateCC: func(cc *v1alpha1.CellCluster) {
				cc.Status.Store.Phase = v1alpha1.UpgradePhase
			},
			upgradingFn: func(lister corelisters.PodLister, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return true, nil
			},
			errExpectFn: nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Proxy.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Proxy.Phase).To(Equal(v1alpha1.NormalPhase))
			},
		},
		{
			name:     "statefulset is not upgrading",
			updateCC: nil,
			upgradingFn: func(lister corelisters.PodLister, set *apps.StatefulSet, cluster *v1alpha1.CellCluster) (bool, error) {
				return false, nil
			},
			errExpectFn: nil,
			ccExpectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.Proxy.StatefulSet.Replicas).To(Equal(int32(3)))
				g.Expect(cc.Status.Proxy.Phase).To(Equal(v1alpha1.NormalPhase))
			},
		},
	}

	for i := range tests {
		t.Logf(tests[i].name)
		testFn(&tests[i], t)
	}
}

func newFakeProxyMemberManager() (*proxyMemberManager, *controller.FakeStatefulSetControl, cache.Indexer, *controller.FakeProxyControl) {
	cli := fake.NewSimpleClientset()
	kubeCli := kubefake.NewSimpleClientset()
	setInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Apps().V1beta1().StatefulSets()
	tcInformer := informers.NewSharedInformerFactory(cli, 0).Deepfabric().V1alpha1().CellClusters()
	svcInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Services()
	podInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Pods()
	setControl := controller.NewFakeStatefulSetControl(setInformer, tcInformer)
	svcControl := controller.NewFakeServiceControl(svcInformer, tcInformer)
	proxyUpgrader := NewFakeProxyUpgrader()
	proxyControl := controller.NewFakeProxyControl()

	pxmm := &proxyMemberManager{
		setControl,
		svcControl,
		proxyControl,
		setInformer.Lister(),
		svcInformer.Lister(),
		podInformer.Lister(),
		proxyUpgrader,
		proxyStatefulSetIsUpgrading,
	}
	return pxmm, setControl, podInformer.Informer().GetIndexer(), proxyControl
}

func newCellClusterForProxy() *v1alpha1.CellCluster {
	return &v1alpha1.CellCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CellCluster",
			APIVersion: "deepfabric.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: corev1.NamespaceDefault,
			UID:       types.UID("test"),
		},
		Spec: v1alpha1.CellClusterSpec{
			Proxy: v1alpha1.ProxySpec{
				ContainerSpec: v1alpha1.ContainerSpec{
					Image: v1alpha1.ProxyMemberType.String(),
					Requests: &v1alpha1.ResourceRequirement{
						CPU:    "1",
						Memory: "2Gi",
					},
				},
				Replicas: 3,
			},
		},
	}
}
