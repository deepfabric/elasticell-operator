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

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	apps "k8s.io/api/apps/v1beta1"
	corelisters "k8s.io/client-go/listers/core/v1"
)

type pdUpgrader struct {
	pdControl  controller.PDControlInterface
	podControl controller.PodControlInterface
	podLister  corelisters.PodLister
}

// NewPDUpgrader returns a pdUpgrader
func NewPDUpgrader(pdControl controller.PDControlInterface,
	podControl controller.PodControlInterface,
	podLister corelisters.PodLister) Upgrader {
	return &pdUpgrader{
		pdControl:  pdControl,
		podControl: podControl,
		podLister:  podLister,
	}
}

func (pu *pdUpgrader) Upgrade(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	force, err := pu.needForceUpgrade(cc)
	if err != nil {
		return err
	}
	if force {
		return pu.forceUpgrade(cc, oldSet, newSet)
	}

	return pu.gracefulUpgrade(cc, oldSet, newSet)
}

func (pu *pdUpgrader) forceUpgrade(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	cc.Status.PD.Phase = v1alpha1.UpgradePhase
	setUpgradePartition(newSet, 0)
	return nil
}

func (pu *pdUpgrader) gracefulUpgrade(cc *v1alpha1.CellCluster, oldSet *apps.StatefulSet, newSet *apps.StatefulSet) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	if !cc.Status.PD.Synced {
		return fmt.Errorf("cellcluster: [%s/%s]'s pd status sync failed,can not to be upgraded", ns, ccName)
	}

	cc.Status.PD.Phase = v1alpha1.UpgradePhase
	if !templateEqual(newSet.Spec.Template, oldSet.Spec.Template) {
		return nil
	}

	setUpgradePartition(newSet, *oldSet.Spec.UpdateStrategy.RollingUpdate.Partition)
	for i := cc.Status.PD.StatefulSet.Replicas - 1; i >= 0; i-- {
		podName := pdPodName(ccName, i)
		pod, err := pu.podLister.Pods(ns).Get(podName)
		if err != nil {
			return err
		}

		revision, exist := pod.Labels[apps.ControllerRevisionHashLabelKey]
		if !exist {
			return controller.RequeueErrorf("cellcluster: [%s/%s]'s pd pod: [%s] has no label: %s", ns, ccName, podName, apps.ControllerRevisionHashLabelKey)
		}

		if revision == cc.Status.PD.StatefulSet.UpdateRevision {
			if member, exist := cc.Status.PD.Members[podName]; !exist || !member.Health {
				return controller.RequeueErrorf("cellcluster: [%s/%s]'s pd upgraded pod: [%s] is not ready", ns, ccName, podName)
			}
			continue
		}

		return pu.upgradePDPod(cc, i, newSet)
	}

	return nil
}

func (pu *pdUpgrader) needForceUpgrade(cc *v1alpha1.CellCluster) (bool, error) {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	instanceName := cc.GetLabels()[label.InstanceLabelKey]
	selector, err := label.New().Instance(instanceName).PD().Selector()
	if err != nil {
		return false, err
	}
	pdPods, err := pu.podLister.Pods(ns).List(selector)
	if err != nil {
		return false, err
	}

	imagePullFailedCount := 0
	for _, pod := range pdPods {
		revisionHash, exist := pod.Labels[apps.ControllerRevisionHashLabelKey]
		if !exist {
			return false, fmt.Errorf("cellcluster: [%s/%s]'s pod:[%s] doesn't have label: %s", ns, ccName, pod.GetName(), apps.ControllerRevisionHashLabelKey)
		}
		if revisionHash == cc.Status.PD.StatefulSet.CurrentRevision {
			if imagePullFailed(pod) {
				imagePullFailedCount++
			}
		}
	}

	return imagePullFailedCount >= int(cc.Status.PD.StatefulSet.Replicas)/2+1, nil
}

func (pu *pdUpgrader) upgradePDPod(cc *v1alpha1.CellCluster, ordinal int32, newSet *apps.StatefulSet) error {
	/*
		ns := cc.GetNamespace()
		ccName := cc.GetName()

		upgradePodName := pdPodName(ccName, ordinal)
		if cc.Status.PD.Leader.Name == upgradePodName && cc.Status.PD.StatefulSet.Replicas > 1 {
			lastOrdinal := cc.Status.PD.StatefulSet.Replicas - 1
			var targetName string
			if ordinal == lastOrdinal {
				targetName = pdPodName(ccName, 0)
			} else {
				targetName = pdPodName(ccName, lastOrdinal)
			}
			err := pu.transferPDLeaderTo(cc, targetName)
			if err != nil {
				return err
			}
			return controller.RequeueErrorf("cellcluster: [%s/%s]'s pd member: [%s] is transferring leader to pd member: [%s]", ns, ccName, upgradePodName, targetName)
		}
	*/

	setUpgradePartition(newSet, ordinal)
	return nil
}

func (pu *pdUpgrader) transferPDLeaderTo(cc *v1alpha1.CellCluster, targetName string) error {
	return pu.pdControl.GetPDClient(cc).TransferPDLeader(targetName)
}

type fakePDUpgrader struct{}

// NewFakePDUpgrader returns a fakePDUpgrader
func NewFakePDUpgrader() Upgrader {
	return &fakePDUpgrader{}
}

func (fpu *fakePDUpgrader) Upgrade(cc *v1alpha1.CellCluster, _ *apps.StatefulSet, _ *apps.StatefulSet) error {
	cc.Status.PD.Phase = v1alpha1.UpgradePhase
	return nil
}
