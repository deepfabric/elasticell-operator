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
	"testing"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	podinformers "k8s.io/client-go/informers/core/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
)

func TestProxyUpgrader_Upgrade(t *testing.T) {
	g := NewGomegaWithT(t)

	type testcase struct {
		name                    string
		changeFn                func(*v1alpha1.CellCluster)
		getLastAppliedConfigErr bool
		errorExpect             bool
		expectFn                func(g *GomegaWithT, cc *v1alpha1.CellCluster, newSet *apps.StatefulSet)
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)
		upgrader, _, podInformer := newProxyUpgrader()
		cc := newCellClusterForProxyUpgrader()
		if test.changeFn != nil {
			test.changeFn(cc)
		}
		pods := getProxyPods()
		for _, pod := range pods {
			podInformer.Informer().GetIndexer().Add(pod)
		}

		oldSet := newStatefulSetForProxyUpgrader()
		newSet := oldSet.DeepCopy()
		if test.getLastAppliedConfigErr {
			oldSet.SetAnnotations(map[string]string{LastAppliedConfigAnnotation: "fake apply config"})
		} else {
			SetLastAppliedConfigAnnotation(oldSet)
		}
		err := upgrader.Upgrade(cc, oldSet, newSet)
		if test.errorExpect {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}
		test.expectFn(g, cc, newSet)
	}

	tests := []*testcase{
		{
			name: "normal",
			changeFn: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.NormalPhase
				cc.Status.Store.Phase = v1alpha1.NormalPhase
			},
			getLastAppliedConfigErr: false,
			expectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster, newSet *apps.StatefulSet) {
				g.Expect(newSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal((func() *int32 { i := int32(0); return &i }())))
			},
		},
		{
			name: "pd is upgrading",
			changeFn: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.UpgradePhase
				cc.Status.Store.Phase = v1alpha1.NormalPhase
			},
			getLastAppliedConfigErr: false,
			expectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster, newSet *apps.StatefulSet) {
				g.Expect(newSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal((func() *int32 { i := int32(1); return &i }())))
			},
		},
		{
			name: "store is upgrading",
			changeFn: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.NormalPhase
				cc.Status.Store.Phase = v1alpha1.UpgradePhase
			},
			getLastAppliedConfigErr: false,
			expectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster, newSet *apps.StatefulSet) {
				g.Expect(newSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal((func() *int32 { i := int32(1); return &i }())))
			},
		},
		{
			name: "get apply config error",
			changeFn: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.NormalPhase
				cc.Status.Store.Phase = v1alpha1.UpgradePhase
			},
			getLastAppliedConfigErr: true,
			errorExpect:             true,
			expectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster, newSet *apps.StatefulSet) {
				g.Expect(newSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal((func() *int32 { i := int32(1); return &i }())))
			},
		},
		{
			name: "upgraded pods are not ready",
			changeFn: func(cc *v1alpha1.CellCluster) {
				cc.Status.PD.Phase = v1alpha1.NormalPhase
				cc.Status.Store.Phase = v1alpha1.NormalPhase
				cc.Status.Proxy.Members["upgrader-proxy-1"] = v1alpha1.ProxyMember{
					Name:   "upgrader-proxy-1",
					Health: false,
				}
			},
			getLastAppliedConfigErr: false,
			errorExpect:             true,
			expectFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster, newSet *apps.StatefulSet) {
				g.Expect(newSet.Spec.UpdateStrategy.RollingUpdate.Partition).To(Equal((func() *int32 { i := int32(1); return &i }())))
			},
		},
	}

	for _, test := range tests {
		testFn(test, t)
	}

}

func newProxyUpgrader() (Upgrader, *controller.FakeProxyControl, podinformers.PodInformer) {
	kubeCli := kubefake.NewSimpleClientset()
	proxyControl := controller.NewFakeProxyControl()
	podInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Pods()
	return &proxyUpgrader{proxyControl: proxyControl, podLister: podInformer.Lister()}, proxyControl, podInformer
}

func newStatefulSetForProxyUpgrader() *apps.StatefulSet {
	return &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "upgrader-proxy",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: apps.StatefulSetSpec{
			Replicas: int32Pointer(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "proxy",
							Image: "proxy-test-image",
						},
					},
				},
			},
			UpdateStrategy: apps.StatefulSetUpdateStrategy{Type: apps.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &apps.RollingUpdateStatefulSetStrategy{
					Partition: int32Pointer(1),
				},
			},
		},
		Status: apps.StatefulSetStatus{
			CurrentRevision: "1",
			UpdateRevision:  "2",
			ReadyReplicas:   2,
			Replicas:        2,
			CurrentReplicas: 1,
			UpdatedReplicas: 1,
		},
	}
}

func newCellClusterForProxyUpgrader() *v1alpha1.CellCluster {
	return &v1alpha1.CellCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CellCluster",
			APIVersion: "deepfabric.com/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "upgrader",
			Namespace: corev1.NamespaceDefault,
			UID:       types.UID("upgrader"),
		},
		Spec: v1alpha1.CellClusterSpec{
			PD: v1alpha1.PDSpec{
				ContainerSpec: v1alpha1.ContainerSpec{
					Image: "pd-test-image",
				},
				Replicas:         3,
				StorageClassName: "my-storage-class",
			},
			Store: v1alpha1.StoreSpec{
				ContainerSpec: v1alpha1.ContainerSpec{
					Image: "store-test-image",
				},
				Replicas:         3,
				StorageClassName: "my-storage-class",
			},
			Proxy: v1alpha1.ProxySpec{
				ContainerSpec: v1alpha1.ContainerSpec{
					Image: "proxy-test-image",
				},
				Replicas:         2,
				StorageClassName: "my-storage-class",
			},
		},
		Status: v1alpha1.CellClusterStatus{
			Proxy: v1alpha1.ProxyStatus{
				StatefulSet: &apps.StatefulSetStatus{
					CurrentReplicas: 1,
					UpdatedReplicas: 1,
					CurrentRevision: "1",
					UpdateRevision:  "2",
					Replicas:        2,
				},
				Members: map[string]v1alpha1.ProxyMember{
					"upgrader-proxy-0": {
						Name:   "upgrader-proxy-0",
						Health: true,
					},
					"upgrader-proxy-1": {
						Name:   "upgrader-proxy-1",
						Health: true,
					},
				},
			},
		},
	}
}

func getProxyPods() []*corev1.Pod {
	lc := label.New().Instance(upgradeInstanceName).Proxy().Labels()
	lc[apps.ControllerRevisionHashLabelKey] = "1"
	lu := label.New().Instance(upgradeInstanceName).Proxy().Labels()
	lu[apps.ControllerRevisionHashLabelKey] = "2"
	pods := []*corev1.Pod{
		{
			TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      proxyPodName(upgradeCcName, 0),
				Namespace: corev1.NamespaceDefault,
				Labels:    lc,
			},
		},
		{
			TypeMeta: metav1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      proxyPodName(upgradeCcName, 1),
				Namespace: corev1.NamespaceDefault,
				Labels:    lu,
			},
		},
	}
	return pods
}
