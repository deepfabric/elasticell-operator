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
	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/manager"
	"github.com/deepfabric/elasticell-operator/pkg/manager/member"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	errorutils "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
)

// ControlInterface implements the control logic for updating CellClusters and their children StatefulSets.
// It is implemented as an interface to allow for extensions that provide different semantics.
// Currently, there is only one implementation.
type ControlInterface interface {
	// UpdateCellCluster implements the control logic for StatefulSet creation, update, and deletion
	UpdateCellCluster(*v1alpha1.CellCluster) error
}

// NewDefaultCellClusterControl returns a new instance of the default implementation CellClusterControlInterface that
// implements the documented semantics for CellClusters.
func NewDefaultCellClusterControl(
	ccControl controller.CellClusterControlInterface,
	pdMemberManager manager.Manager,
	storeMemberManager manager.Manager,
	proxyMemberManager manager.Manager,
	reclaimPolicyManager manager.Manager,
	metaManager manager.Manager,
	orphanPodsCleaner member.OrphanPodsCleaner,
	recorder record.EventRecorder) ControlInterface {
	return &defaultCellClusterControl{
		ccControl,
		pdMemberManager,
		storeMemberManager,
		proxyMemberManager,
		reclaimPolicyManager,
		metaManager,
		orphanPodsCleaner,
		recorder,
	}
}

type defaultCellClusterControl struct {
	ccControl            controller.CellClusterControlInterface
	pdMemberManager      manager.Manager
	storeMemberManager   manager.Manager
	proxyMemberManager   manager.Manager
	reclaimPolicyManager manager.Manager
	metaManager          manager.Manager
	orphanPodsCleaner    member.OrphanPodsCleaner
	recorder             record.EventRecorder
}

// UpdateStatefulSet executes the core logic loop for a cellcluster.
func (ccc *defaultCellClusterControl) UpdateCellCluster(cc *v1alpha1.CellCluster) error {
	var errs []error
	oldStatus := cc.Status.DeepCopy()

	if err := ccc.updateCellCluster(cc); err != nil {
		errs = append(errs, err)
	}
	if apiequality.Semantic.DeepEqual(&cc.Status, oldStatus) {
		return errorutils.NewAggregate(errs)
	}
	if _, err := ccc.ccControl.UpdateCellCluster(cc.DeepCopy(), &cc.Status, oldStatus); err != nil {
		errs = append(errs, err)
	}

	return errorutils.NewAggregate(errs)
}

func (ccc *defaultCellClusterControl) updateCellCluster(cc *v1alpha1.CellCluster) error {
	// syncing all PVs managed by operator's reclaim policy to Retain
	if err := ccc.reclaimPolicyManager.Sync(cc); err != nil {
		return err
	}

	// cleaning all orphan pods(pd or store which don't have a related PVC) managed by operator
	if _, err := ccc.orphanPodsCleaner.Clean(cc); err != nil {
		return err
	}

	// works that should do to making the pd cluster current state match the desired state:
	//   - create or update the pd service
	//   - create or update the pd headless service
	//   - create the pd statefulset
	//   - sync pd cluster status from pd to CellCluster object
	//   - set two annotations to the first pd member:
	// 	   - label.Bootstrapping
	// 	   - label.Replicas
	//   - upgrade the pd cluster
	//   - scale out/in the pd cluster
	//   - failover the pd cluster
	if err := ccc.pdMemberManager.Sync(cc); err != nil {
		return err
	}

	// works that should do to making the store cluster current state match the desired state:
	//   - waiting for the pd cluster available(pd cluster is in quorum)
	//   - create or update store headless service
	//   - create the store statefulset
	//   - sync store cluster status from pd to CellCluster object
	//   - set scheduler labels to store stores
	//   - upgrade the store cluster
	//   - scale out/in the store cluster
	//   - failover the store cluster
	if err := ccc.storeMemberManager.Sync(cc); err != nil {
		return err
	}

	// works that should do to making the proxy cluster current state match the desired state:
	//   - waiting for the store cluster available(at least one peer works)
	//   - create or update proxy headless service
	//   - create the proxy statefulset
	//   - sync proxy cluster status from pd to CellCluster object
	//   - upgrade the proxy cluster
	//   - scale out/in the proxy cluster
	//   - failover the proxy cluster
	if err := ccc.proxyMemberManager.Sync(cc); err != nil {
		return err
	}

	// syncing the labels from Pod to PVC and PV, these labels include:
	//   - label.StoreIDLabelKey
	//   - label.MemberIDLabelKey
	//   - label.NamespaceLabelKey
	return ccc.metaManager.Sync(cc)
}
