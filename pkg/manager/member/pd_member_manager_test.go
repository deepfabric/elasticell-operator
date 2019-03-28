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
	"k8s.io/client-go/tools/cache"
)

func TestPDMemberManagerSyncCreate(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                       string
		prepare                    func(cluster *v1alpha1.CellCluster)
		errWhenCreateStatefulSet   bool
		errWhenCreatePDService     bool
		errWhenCreatePDPeerService bool
		errExpectFn                func(*GomegaWithT, error)
		pdSvcCreated               bool
		setCreated                 bool
	}

	testFn := func(test *testcase, t *testing.T) {
		t.Log(test.name)
		cc := newCellClusterForPD()
		ns := cc.Namespace
		ccName := cc.Name
		oldSpec := cc.Spec
		if test.prepare != nil {
			test.prepare(cc)
		}

		pdmm, fakeSetControl, fakeSvcControl, fakePDControl, _, _, _ := newFakePDMemberManager()
		pdClient := controller.NewFakePDClient()
		fakePDControl.SetPDClient(cc, pdClient)

		if test.errWhenCreateStatefulSet {
			fakeSetControl.SetCreateStatefulSetError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}
		if test.errWhenCreatePDService {
			fakeSvcControl.SetCreateServiceError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}
		if test.errWhenCreatePDPeerService {
			fakeSvcControl.SetCreateServiceError(errors.NewInternalError(fmt.Errorf("API server failed")), 1)
		}

		err := pdmm.Sync(cc)
		test.errExpectFn(g, err)
		g.Expect(cc.Spec).To(Equal(oldSpec))

		svc1, err := pdmm.svcLister.Services(ns).Get(controller.PDMemberName(ccName))
		if test.pdSvcCreated {
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(svc1).NotTo(Equal(nil))
		} else {
			expectErrIsNotFound(g, err)
		}

		tc1, err := pdmm.setLister.StatefulSets(ns).Get(controller.PDMemberName(ccName))
		if test.setCreated {
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(tc1).NotTo(Equal(nil))
		} else {
			expectErrIsNotFound(g, err)
		}
	}

	tests := []testcase{
		{
			name:                       "normal",
			prepare:                    nil,
			errWhenCreateStatefulSet:   false,
			errWhenCreatePDService:     false,
			errWhenCreatePDPeerService: false,
			errExpectFn:                errExpectRequeue,
			pdSvcCreated:               true,
			setCreated:                 true,
		},
		{
			name: "cellcluster's storage format is wrong",
			prepare: func(cc *v1alpha1.CellCluster) {
				cc.Spec.PD.Requests.Storage = "100xxxxi"
			},
			errWhenCreateStatefulSet:   false,
			errWhenCreatePDService:     false,
			errWhenCreatePDPeerService: false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "cant' get storage size: 100xxxxi for CellCluster: default/test")).To(BeTrue())
			},
			pdSvcCreated: true,
			setCreated:   false,
		},
		{
			name:                       "error when create statefulset",
			prepare:                    nil,
			errWhenCreateStatefulSet:   true,
			errWhenCreatePDService:     false,
			errWhenCreatePDPeerService: false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "API server failed")).To(BeTrue())
			},
			pdSvcCreated: true,
			setCreated:   false,
		},
		{
			name:                       "error when create pd service",
			prepare:                    nil,
			errWhenCreateStatefulSet:   false,
			errWhenCreatePDService:     true,
			errWhenCreatePDPeerService: false,
			errExpectFn: func(g *GomegaWithT, err error) {
				g.Expect(err).To(HaveOccurred())
				g.Expect(strings.Contains(err.Error(), "API server failed")).To(BeTrue())
			},
			pdSvcCreated: false,
			setCreated:   false,
		},
	}

	for i := range tests {
		testFn(&tests[i], t)
	}
}

func TestPDMemberManagerSyncUpdate(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                     string
		modify                   func(cluster *v1alpha1.CellCluster)
		pdHealth                 *controller.HealthInfo
		errWhenUpdateStatefulSet bool
		errWhenUpdatePDService   bool
		errWhenGetCluster        bool
		errWhenGetPDHealth       bool
		statusChange             func(*apps.StatefulSet)
		err                      bool
		expectPDServiceFn        func(*GomegaWithT, *corev1.Service, error)
		expectStatefulSetFn      func(*GomegaWithT, *apps.StatefulSet, error)
		expectCellClusterFn      func(*GomegaWithT, *v1alpha1.CellCluster)
	}

	testFn := func(test *testcase, t *testing.T) {
		cc := newCellClusterForPD()
		ns := cc.Namespace
		ccName := cc.Name

		pdmm, fakeSetControl, fakeSvcControl, fakePDControl, _, _, _ := newFakePDMemberManager()
		pdClient := controller.NewFakePDClient()
		fakePDControl.SetPDClient(cc, pdClient)
		if test.errWhenGetPDHealth {
			pdClient.AddReaction(controller.GetHealthActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get health of pd cluster")
			})
		} else {
			pdClient.AddReaction(controller.GetHealthActionType, func(action *controller.Action) (interface{}, error) {
				return test.pdHealth, nil
			})
		}

		if test.errWhenGetCluster {
			pdClient.AddReaction(controller.GetClusterActionType, func(action *controller.Action) (interface{}, error) {
				return nil, fmt.Errorf("failed to get cluster info")
			})
		} else {
			pdClient.AddReaction(controller.GetClusterActionType, func(action *controller.Action) (interface{}, error) {
				return "elasticell-cluster-demo", nil
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

		err := pdmm.Sync(cc)
		g.Expect(controller.IsRequeueError(err)).To(BeTrue())

		_, err = pdmm.svcLister.Services(ns).Get(controller.PDMemberName(ccName))
		g.Expect(err).NotTo(HaveOccurred())
		_, err = pdmm.setLister.StatefulSets(ns).Get(controller.PDMemberName(ccName))
		g.Expect(err).NotTo(HaveOccurred())

		tc1 := cc.DeepCopy()
		test.modify(tc1)

		if test.errWhenUpdatePDService {
			fakeSvcControl.SetUpdateServiceError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}
		if test.errWhenUpdateStatefulSet {
			fakeSetControl.SetUpdateStatefulSetError(errors.NewInternalError(fmt.Errorf("API server failed")), 0)
		}

		err = pdmm.Sync(tc1)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}

		if test.expectPDServiceFn != nil {
			svc, err := pdmm.svcLister.Services(ns).Get(controller.PDMemberName(ccName))
			test.expectPDServiceFn(g, svc, err)
		}
		if test.expectStatefulSetFn != nil {
			set, err := pdmm.setLister.StatefulSets(ns).Get(controller.PDMemberName(ccName))
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
				cc.Spec.PD.Replicas = 5
				cc.Spec.Services = []v1alpha1.Service{
					{Name: "pd", Type: string(corev1.ServiceTypeNodePort)},
				}
			},
			pdHealth: &controller.HealthInfo{Healths: []controller.MemberHealth{
				{Name: "pd1", MemberID: uint64(1), ClientUrls: []string{"http://pd1:2379"}, Health: true},
				{Name: "pd2", MemberID: uint64(2), ClientUrls: []string{"http://pd2:2379"}, Health: true},
				{Name: "pd3", MemberID: uint64(3), ClientUrls: []string{"http://pd3:2379"}, Health: false},
			}},
			errWhenUpdateStatefulSet: false,
			errWhenUpdatePDService:   false,
			errWhenGetPDHealth:       false,
			err:                      false,
			expectPDServiceFn: func(g *GomegaWithT, svc *corev1.Service, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
			},
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				// g.Expect(int(*set.Spec.Replicas)).To(Equal(4))
			},
			expectCellClusterFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.ClusterID).To(Equal("elasticell-cluster-demo"))
				g.Expect(cc.Status.PD.Phase).To(Equal(v1alpha1.NormalPhase))
				g.Expect(*cc.Status.PD.StatefulSet.ObservedGeneration).To(Equal(int64(1)))
				g.Expect(len(cc.Status.PD.Members)).To(Equal(3))
				g.Expect(cc.Status.PD.Members["pd1"].Health).To(Equal(true))
				g.Expect(cc.Status.PD.Members["pd2"].Health).To(Equal(true))
				g.Expect(cc.Status.PD.Members["pd3"].Health).To(Equal(false))
			},
		},
		{
			name: "cellcluster's storage format is wrong",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.PD.Requests.Storage = "100xxxxi"
			},
			pdHealth: &controller.HealthInfo{Healths: []controller.MemberHealth{
				{Name: "pd1", MemberID: uint64(1), ClientUrls: []string{"http://pd1:2379"}, Health: true},
				{Name: "pd2", MemberID: uint64(2), ClientUrls: []string{"http://pd2:2379"}, Health: true},
				{Name: "pd3", MemberID: uint64(3), ClientUrls: []string{"http://pd3:2379"}, Health: false},
			}},
			errWhenUpdateStatefulSet: false,
			errWhenUpdatePDService:   false,
			err:                      true,
			expectPDServiceFn:        nil,
			expectStatefulSetFn:      nil,
		},
		{
			name: "error when update pd service",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.Services = []v1alpha1.Service{
					{Name: "pd", Type: string(corev1.ServiceTypeNodePort)},
				}
			},
			pdHealth: &controller.HealthInfo{Healths: []controller.MemberHealth{
				{Name: "pd1", MemberID: uint64(1), ClientUrls: []string{"http://pd1:2379"}, Health: true},
				{Name: "pd2", MemberID: uint64(2), ClientUrls: []string{"http://pd2:2379"}, Health: true},
				{Name: "pd3", MemberID: uint64(3), ClientUrls: []string{"http://pd3:2379"}, Health: false},
			}},
			errWhenUpdateStatefulSet: false,
			errWhenUpdatePDService:   true,
			err:                      true,
			expectPDServiceFn:        nil,
			expectStatefulSetFn:      nil,
		},
		{
			name: "error when update statefulset",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.PD.Replicas = 5
			},
			pdHealth: &controller.HealthInfo{Healths: []controller.MemberHealth{
				{Name: "pd1", MemberID: uint64(1), ClientUrls: []string{"http://pd1:2379"}, Health: true},
				{Name: "pd2", MemberID: uint64(2), ClientUrls: []string{"http://pd2:2379"}, Health: true},
				{Name: "pd3", MemberID: uint64(3), ClientUrls: []string{"http://pd3:2379"}, Health: false},
			}},
			errWhenUpdateStatefulSet: true,
			errWhenUpdatePDService:   false,
			err:                      true,
			expectPDServiceFn:        nil,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
		},
		{
			name: "error when sync pd status",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.PD.Replicas = 5
			},
			errWhenUpdateStatefulSet: false,
			errWhenUpdatePDService:   false,
			errWhenGetPDHealth:       true,
			err:                      false,
			expectPDServiceFn:        nil,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			expectCellClusterFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.PD.Synced).To(BeFalse())
				g.Expect(cc.Status.PD.Members).To(BeNil())
			},
		},
		{
			name: "error when sync cluster ID",
			modify: func(cc *v1alpha1.CellCluster) {
				cc.Spec.PD.Replicas = 5
			},
			errWhenUpdateStatefulSet: false,
			errWhenUpdatePDService:   false,
			errWhenGetCluster:        true,
			errWhenGetPDHealth:       false,
			err:                      false,
			expectPDServiceFn:        nil,
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
			},
			expectCellClusterFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.PD.Synced).To(BeFalse())
				g.Expect(cc.Status.PD.Members).To(BeNil())
			},
		},
	}

	for i := range tests {
		t.Logf("begin: %s", tests[i].name)
		testFn(&tests[i], t)
		t.Logf("end: %s", tests[i].name)
	}
}

func TestPDMemberManagerPdStatefulSetIsUpgrading(t *testing.T) {
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
		pdmm, _, _, _, podIndexer, _, _ := newFakePDMemberManager()
		cc := newCellClusterForPD()
		cc.Status.PD.StatefulSet = &apps.StatefulSetStatus{
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
					Name:        ordinalPodName(v1alpha1.PDMemberType, cc.GetName(), 0),
					Namespace:   metav1.NamespaceDefault,
					Annotations: map[string]string{},
					Labels:      label.New().Instance(cc.GetLabels()[label.InstanceLabelKey]).PD().Labels(),
				},
			}
			if test.updatePod != nil {
				test.updatePod(pod)
			}
			podIndexer.Add(pod)
		}
		b, err := pdmm.pdStatefulSetIsUpgrading(set, cc)
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

func TestPDMemberManagerUpgrade(t *testing.T) {
	g := NewGomegaWithT(t)
	type testcase struct {
		name                string
		modify              func(cluster *v1alpha1.CellCluster)
		pdHealth            *controller.HealthInfo
		err                 bool
		statusChange        func(*apps.StatefulSet)
		expectStatefulSetFn func(*GomegaWithT, *apps.StatefulSet, error)
		expectCellClusterFn func(*GomegaWithT, *v1alpha1.CellCluster)
	}

	testFn := func(test *testcase, t *testing.T) {
		cc := newCellClusterForPD()
		ns := cc.Namespace
		ccName := cc.Name

		pdmm, fakeSetControl, _, fakePDControl, _, _, _ := newFakePDMemberManager()
		pdClient := controller.NewFakePDClient()
		fakePDControl.SetPDClient(cc, pdClient)

		pdClient.AddReaction(controller.GetHealthActionType, func(action *controller.Action) (interface{}, error) {
			return test.pdHealth, nil
		})
		pdClient.AddReaction(controller.GetClusterActionType, func(action *controller.Action) (interface{}, error) {
			return "1", nil
		})

		fakeSetControl.SetStatusChange(test.statusChange)

		err := pdmm.Sync(cc)
		g.Expect(controller.IsRequeueError(err)).To(BeTrue())

		_, err = pdmm.svcLister.Services(ns).Get(controller.PDMemberName(ccName))
		g.Expect(err).NotTo(HaveOccurred())
		_, err = pdmm.setLister.StatefulSets(ns).Get(controller.PDMemberName(ccName))
		g.Expect(err).NotTo(HaveOccurred())

		tc1 := cc.DeepCopy()
		test.modify(tc1)

		err = pdmm.Sync(tc1)
		if test.err {
			g.Expect(err).To(HaveOccurred())
		} else {
			g.Expect(err).NotTo(HaveOccurred())
		}

		if test.expectStatefulSetFn != nil {
			set, err := pdmm.setLister.StatefulSets(ns).Get(controller.PDMemberName(ccName))
			test.expectStatefulSetFn(g, set, err)
		}
		if test.expectCellClusterFn != nil {
			test.expectCellClusterFn(g, tc1)
		}
	}
	tests := []testcase{
		{
			name: "upgrade successful",
			modify: func(cluster *v1alpha1.CellCluster) {
				cluster.Spec.PD.Image = "pd-test-image:v2"
			},
			pdHealth: &controller.HealthInfo{Healths: []controller.MemberHealth{
				{Name: "pd1", MemberID: uint64(1), ClientUrls: []string{"http://pd1:2379"}, Health: true},
				{Name: "pd2", MemberID: uint64(2), ClientUrls: []string{"http://pd2:2379"}, Health: true},
				{Name: "pd3", MemberID: uint64(3), ClientUrls: []string{"http://pd3:2379"}, Health: false},
			}},
			err: false,
			statusChange: func(set *apps.StatefulSet) {
				set.Status.Replicas = *set.Spec.Replicas
				set.Status.CurrentRevision = "pd-1"
				set.Status.UpdateRevision = "pd-1"
				observedGeneration := int64(1)
				set.Status.ObservedGeneration = &observedGeneration
			},
			expectStatefulSetFn: func(g *GomegaWithT, set *apps.StatefulSet, err error) {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(set.Spec.Template.Spec.Containers[0].Image).To(Equal("pd-test-image:v2"))
			},
			expectCellClusterFn: func(g *GomegaWithT, cc *v1alpha1.CellCluster) {
				g.Expect(cc.Status.PD.Phase).To(Equal(v1alpha1.UpgradePhase))
				g.Expect(len(cc.Status.PD.Members)).To(Equal(3))
				g.Expect(cc.Status.PD.Members["pd1"].Health).To(Equal(true))
				g.Expect(cc.Status.PD.Members["pd2"].Health).To(Equal(true))
				g.Expect(cc.Status.PD.Members["pd3"].Health).To(Equal(false))
			},
		},
	}
	for i := range tests {
		t.Logf("begin: %s", tests[i].name)
		testFn(&tests[i], t)
		t.Logf("end: %s", tests[i].name)
	}
}

func newFakePDMemberManager() (*pdMemberManager, *controller.FakeStatefulSetControl, *controller.FakeServiceControl, *controller.FakePDControl, cache.Indexer, cache.Indexer, *controller.FakePodControl) {
	cli := fake.NewSimpleClientset()
	kubeCli := kubefake.NewSimpleClientset()
	setInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Apps().V1beta1().StatefulSets()
	svcInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Services()
	podInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().Pods()
	pvcInformer := kubeinformers.NewSharedInformerFactory(kubeCli, 0).Core().V1().PersistentVolumeClaims()
	tcInformer := informers.NewSharedInformerFactory(cli, 0).Deepfabric().V1alpha1().CellClusters()
	setControl := controller.NewFakeStatefulSetControl(setInformer, tcInformer)
	svcControl := controller.NewFakeServiceControl(svcInformer, tcInformer)
	podControl := controller.NewFakePodControl(podInformer)
	pdControl := controller.NewFakePDControl()
	pdUpgrader := NewFakePDUpgrader()

	return &pdMemberManager{
		pdControl,
		setControl,
		svcControl,
		setInformer.Lister(),
		svcInformer.Lister(),
		podInformer.Lister(),
		podControl,
		pvcInformer.Lister(),
		pdUpgrader,
	}, setControl, svcControl, pdControl, podInformer.Informer().GetIndexer(), pvcInformer.Informer().GetIndexer(), podControl
}

func newCellClusterForPD() *v1alpha1.CellCluster {
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
			PD: v1alpha1.PDSpec{
				ContainerSpec: v1alpha1.ContainerSpec{
					Image: "pd-test-image",
					Requests: &v1alpha1.ResourceRequirement{
						CPU:     "1",
						Memory:  "2Gi",
						Storage: "100Gi",
					},
				},
				Replicas:         3,
				StorageClassName: "my-storage-class",
			},
			Store: v1alpha1.StoreSpec{
				ContainerSpec: v1alpha1.ContainerSpec{
					Image: "store-test-image",
					Requests: &v1alpha1.ResourceRequirement{
						CPU:     "1",
						Memory:  "2Gi",
						Storage: "100Gi",
					},
				},
				Replicas:         3,
				StorageClassName: "my-storage-class",
			},
		},
	}
}

func expectErrIsNotFound(g *GomegaWithT, err error) {
	g.Expect(err).NotTo(Equal(nil))
	g.Expect(errors.IsNotFound(err)).To(Equal(true))
}

func errExpectNil(g *GomegaWithT, err error) {
	g.Expect(err).NotTo(HaveOccurred())
}
