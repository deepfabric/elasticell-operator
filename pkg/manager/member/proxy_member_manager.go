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
	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	"github.com/deepfabric/elasticell-operator/pkg/manager"
	"github.com/deepfabric/elasticell-operator/pkg/util"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/listers/apps/v1beta1"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type proxyMemberManager struct {
	setControl                    controller.StatefulSetControlInterface
	svcControl                    controller.ServiceControlInterface
	proxyControl                  controller.ProxyControlInterface
	setLister                     v1beta1.StatefulSetLister
	svcLister                     corelisters.ServiceLister
	podLister                     corelisters.PodLister
	proxyUpgrader                 Upgrader
	proxyStatefulSetIsUpgradingFn func(corelisters.PodLister, *apps.StatefulSet, *v1alpha1.CellCluster) (bool, error)
}

// NewProxyMemberManager returns a *proxyMemberManager
func NewProxyMemberManager(setControl controller.StatefulSetControlInterface,
	svcControl controller.ServiceControlInterface,
	proxyControl controller.ProxyControlInterface,
	setLister v1beta1.StatefulSetLister,
	svcLister corelisters.ServiceLister,
	podLister corelisters.PodLister,
	proxyUpgrader Upgrader) manager.Manager {
	return &proxyMemberManager{
		setControl:                    setControl,
		svcControl:                    svcControl,
		proxyControl:                  proxyControl,
		setLister:                     setLister,
		svcLister:                     svcLister,
		podLister:                     podLister,
		proxyUpgrader:                 proxyUpgrader,
		proxyStatefulSetIsUpgradingFn: proxyStatefulSetIsUpgrading,
	}
}

func (pxmm *proxyMemberManager) Sync(cc *v1alpha1.CellCluster) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()

	if !cc.StoreIsAvailable() {
		return controller.RequeueErrorf("CellCluster: [%s/%s], waiting for Store cluster running", ns, ccName)
	}
	// Sync proxy StatefulSet
	return pxmm.syncProxyStatefulSetForCellCluster(cc)
}

func (pxmm *proxyMemberManager) syncProxyStatefulSetForCellCluster(cc *v1alpha1.CellCluster) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()

	newProxySet := pxmm.getNewProxySetForCellCluster(cc)
	oldProxySet, err := pxmm.setLister.StatefulSets(ns).Get(controller.ProxyMemberName(ccName))
	if errors.IsNotFound(err) {
		err = SetLastAppliedConfigAnnotation(newProxySet)
		if err != nil {
			return err
		}
		err = pxmm.setControl.CreateStatefulSet(cc, newProxySet)
		if err != nil {
			return err
		}
		cc.Status.Proxy.StatefulSet = &apps.StatefulSetStatus{}
		return nil
	}
	if err != nil {
		return err
	}

	if err = pxmm.syncCellClusterStatus(cc, oldProxySet); err != nil {
		return err
	}

	if !templateEqual(newProxySet.Spec.Template, oldProxySet.Spec.Template) || cc.Status.Proxy.Phase == v1alpha1.UpgradePhase {
		if err := pxmm.proxyUpgrader.Upgrade(cc, oldProxySet, newProxySet); err != nil {
			return err
		}
	}

	if !statefulSetEqual(*newProxySet, *oldProxySet) {
		set := *oldProxySet
		set.Spec.Template = newProxySet.Spec.Template
		*set.Spec.Replicas = *newProxySet.Spec.Replicas
		set.Spec.UpdateStrategy = newProxySet.Spec.UpdateStrategy
		err := SetLastAppliedConfigAnnotation(&set)
		if err != nil {
			return err
		}
		_, err = pxmm.setControl.UpdateStatefulSet(cc, &set)
		return err
	}

	return nil
}

func (pxmm *proxyMemberManager) getNewProxySetForCellCluster(cc *v1alpha1.CellCluster) *apps.StatefulSet {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	instanceName := cc.GetLabels()[label.InstanceLabelKey]
	proxyConfigMap := controller.ProxyMemberName(ccName)

	annMount, annVolume := annotationsMountVolume()
	volMounts := []corev1.VolumeMount{
		annMount,
		{Name: "startup-script", ReadOnly: true, MountPath: "/usr/local/bin/startup"},
	}
	vols := []corev1.Volume{
		annVolume,
		{Name: "startup-script", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: proxyConfigMap,
				},
				Items: []corev1.KeyToPath{{Key: "startup-script", Path: "proxy_start_script.sh"}},
			}},
		},
	}

	var containers []corev1.Container

	envs := []corev1.EnvVar{
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name:  "CLUSTER_NAME",
			Value: cc.GetName(),
		},
		{
			Name:  "TZ",
			Value: cc.Spec.Timezone,
		},
	}

	containers = append(containers, corev1.Container{
		Name:            v1alpha1.ProxyMemberType.String(),
		Image:           cc.Spec.Proxy.Image,
		Command:         []string{"/bin/sh", "/usr/local/bin/startup/proxy_start_script.sh"},
		ImagePullPolicy: cc.Spec.Proxy.ImagePullPolicy,
		Ports: []corev1.ContainerPort{
			{
				Name:          "redis",
				ContainerPort: int32(6379),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: volMounts,
		Resources:    util.ResourceRequirement(cc.Spec.Proxy.ContainerSpec),
		Env:          envs,
	})

	proxyLabel := label.New().Instance(instanceName).Proxy()
	proxySet := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            controller.ProxyMemberName(ccName),
			Namespace:       ns,
			Labels:          proxyLabel.Labels(),
			OwnerReferences: []metav1.OwnerReference{controller.GetOwnerRef(cc)},
		},
		Spec: apps.StatefulSetSpec{
			Replicas: func() *int32 { r := cc.ProxyRealReplicas(); return &r }(),
			Selector: proxyLabel.LabelSelector(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: proxyLabel.Labels()},
				Spec: corev1.PodSpec{
					SchedulerName: cc.Spec.SchedulerName,
					Affinity: util.AffinityForNodeSelector(
						ns,
						cc.Spec.Proxy.NodeSelectorRequired,
						label.New().Instance(instanceName).Proxy(),
						cc.Spec.Proxy.NodeSelector,
					),
					Containers:    containers,
					RestartPolicy: corev1.RestartPolicyAlways,
					Tolerations:   cc.Spec.Proxy.Tolerations,
					Volumes:       vols,
				},
			},
			PodManagementPolicy: apps.ParallelPodManagement,
			UpdateStrategy: apps.StatefulSetUpdateStrategy{Type: apps.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &apps.RollingUpdateStatefulSetStrategy{Partition: func() *int32 { r := cc.ProxyRealReplicas(); return &r }()},
			},
		},
	}
	return proxySet
}

func (pxmm *proxyMemberManager) syncCellClusterStatus(cc *v1alpha1.CellCluster, set *apps.StatefulSet) error {
	cc.Status.Proxy.StatefulSet = &set.Status

	upgrading, err := pxmm.proxyStatefulSetIsUpgradingFn(pxmm.podLister, set, cc)
	if err != nil {
		return err
	}
	if upgrading && cc.Status.Store.Phase != v1alpha1.UpgradePhase && cc.Status.PD.Phase != v1alpha1.UpgradePhase {
		cc.Status.Proxy.Phase = v1alpha1.UpgradePhase
	} else {
		cc.Status.Proxy.Phase = v1alpha1.NormalPhase
	}

	return nil
}

func proxyStatefulSetIsUpgrading(podLister corelisters.PodLister, set *apps.StatefulSet, cc *v1alpha1.CellCluster) (bool, error) {
	if statefulSetIsUpgrading(set) {
		return true, nil
	}
	selector, err := label.New().
		Instance(cc.GetLabels()[label.InstanceLabelKey]).
		Proxy().
		Selector()
	if err != nil {
		return false, err
	}
	proxyPods, err := podLister.Pods(cc.GetNamespace()).List(selector)
	if err != nil {
		return false, err
	}
	for _, pod := range proxyPods {
		revisionHash, exist := pod.Labels[apps.ControllerRevisionHashLabelKey]
		if !exist {
			return false, nil
		}
		if revisionHash != cc.Status.Proxy.StatefulSet.UpdateRevision {
			return true, nil
		}
	}
	return false, nil
}

type FakeProxyMemberManager struct {
	err error
}

func NewFakeProxyMemberManager() *FakeProxyMemberManager {
	return &FakeProxyMemberManager{}
}

func (ftmm *FakeProxyMemberManager) SetSyncError(err error) {
	ftmm.err = err
}

func (ftmm *FakeProxyMemberManager) Sync(_ *v1alpha1.CellCluster) error {
	if ftmm.err != nil {
		return ftmm.err
	}
	return nil
}
