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
	apps "k8s.io/api/apps/v1beta1"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type proxyUpgrader struct {
	podLister    corelisters.PodLister
	proxyControl controller.ProxyControlInterface
}

// NewProxyUpgrader returns a proxy Upgrader
func NewProxyUpgrader(proxyControl controller.ProxyControlInterface, podLister corelisters.PodLister) Upgrader {
	return &proxyUpgrader{
		proxyControl: proxyControl,
		podLister:    podLister,
	}
}

func (pxu *proxyUpgrader) Upgrade(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()

	if cc.Status.PD.Phase == v1alpha1.UpgradePhase || cc.Status.Store.Phase == v1alpha1.UpgradePhase {
		_, podSpec, err := GetLastAppliedConfig(oldSet)
		if err != nil {
			return err
		}
		newSet.Spec.Template.Spec = *podSpec
		return nil
	}

	cc.Status.Proxy.Phase = v1alpha1.UpgradePhase
	if !templateEqual(newSet.Spec.Template, oldSet.Spec.Template) {
		return nil
	}

	setUpgradePartition(newSet, *oldSet.Spec.UpdateStrategy.RollingUpdate.Partition)
	for i := cc.Status.Proxy.StatefulSet.Replicas - 1; i >= 0; i-- {
		podName := proxyPodName(ccName, i)
		pod, err := pxu.podLister.Pods(ns).Get(podName)
		if err != nil {
			return err
		}
		revision, exist := pod.Labels[apps.ControllerRevisionHashLabelKey]
		if !exist {
			return controller.RequeueErrorf("cellcluster: [%s/%s]'s proxy pod: [%s] has no label: %s", ns, ccName, podName, apps.ControllerRevisionHashLabelKey)
		}

		if revision == cc.Status.Proxy.StatefulSet.UpdateRevision {
			if member, exist := cc.Status.Proxy.Members[podName]; !exist || !member.Health {
				return controller.RequeueErrorf("cellcluster: [%s/%s]'s proxy upgraded pod: [%s] is not ready", ns, ccName, podName)
			}
			continue
		}
		return pxu.upgradeProxyPod(cc, i, newSet)
	}

	return nil
}

func (pxu *proxyUpgrader) upgradeProxyPod(cc *v1alpha1.CellCluster, ordinal int32, newSet *apps.StatefulSet) error {
	setUpgradePartition(newSet, ordinal)
	return nil
}

type fakeProxyUpgrader struct{}

// NewFakeProxyUpgrader returns a fake proxy upgrader
func NewFakeProxyUpgrader() Upgrader {
	return &fakeProxyUpgrader{}
}

func (fpxu *fakeProxyUpgrader) Upgrade(cc *v1alpha1.CellCluster, _ *apps.StatefulSet, _ *apps.StatefulSet) error {
	cc.Status.Proxy.Phase = v1alpha1.UpgradePhase
	return nil
}
