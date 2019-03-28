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

func (mt MemberType) String() string {
	return string(mt)
}

func (cc *CellCluster) PDUpgrading() bool {
	return cc.Status.PD.Phase == UpgradePhase
}

func (cc *CellCluster) StoreUpgrading() bool {
	return cc.Status.Store.Phase == UpgradePhase
}

func (cc *CellCluster) PDAllPodsStarted() bool {
	return cc.PDRealReplicas() == cc.Status.PD.StatefulSet.Replicas
}

func (cc *CellCluster) PDAllMembersReady() bool {
	if int(cc.PDRealReplicas()) != len(cc.Status.PD.Members) {
		return false
	}

	for _, member := range cc.Status.PD.Members {
		if !member.Health {
			return false
		}
	}
	return true
}

func (cc *CellCluster) PDAutoFailovering() bool {
	if len(cc.Status.PD.FailureMembers) == 0 {
		return false
	}

	for _, failureMember := range cc.Status.PD.FailureMembers {
		if !failureMember.MemberDeleted {
			return true
		}
	}
	return false
}

func (cc *CellCluster) PDRealReplicas() int32 {
	return cc.Spec.PD.Replicas + int32(len(cc.Status.PD.FailureMembers))
}

func (cc *CellCluster) StoreAllPodsStarted() bool {
	return cc.StoreRealReplicas() == cc.Status.Store.StatefulSet.Replicas
}

func (cc *CellCluster) StoreAllStoresReady() bool {
	if int(cc.StoreRealReplicas()) != len(cc.Status.Store.Stores) {
		return false
	}

	for _, store := range cc.Status.Store.Stores {
		if store.State != StoreStateUp {
			return false
		}
	}

	return true
}

func (cc *CellCluster) StoreRealReplicas() int32 {
	return cc.Spec.Store.Replicas + int32(len(cc.Status.Store.FailureStores))
}

func (cc *CellCluster) ProxyAllPodsStarted() bool {
	return cc.ProxyRealReplicas() == cc.Status.Proxy.StatefulSet.Replicas
}

func (cc *CellCluster) ProxyAllMembersReady() bool {
	/*
		if int(cc.ProxyRealReplicas()) != len(cc.Status.Proxy.Members) {
			return false
		}

		for _, member := range cc.Status.Proxy.Members {
			if !member.Health {
				return false
			}
		}
	*/
	return true
}

func (cc *CellCluster) ProxyRealReplicas() int32 {
	return cc.Spec.Proxy.Replicas + int32(len(cc.Status.Proxy.FailureMembers))
}

func (cc *CellCluster) PDIsAvailable() bool {
	lowerLimit := cc.Spec.PD.Replicas/2 + 1
	if int32(len(cc.Status.PD.Members)) < lowerLimit {
		return false
	}

	var availableNum int32
	for _, pdMember := range cc.Status.PD.Members {
		if pdMember.Health {
			availableNum++
		}
	}

	if availableNum < lowerLimit {
		return false
	}

	if cc.Status.PD.StatefulSet == nil || cc.Status.PD.StatefulSet.ReadyReplicas < lowerLimit {
		return false
	}

	return true
}

func (cc *CellCluster) StoreIsAvailable() bool {
	var lowerLimit int32 = 1
	if int32(len(cc.Status.Store.Stores)) < lowerLimit {
		return false
	}

	var availableNum int32
	for _, store := range cc.Status.Store.Stores {
		if store.State == StoreStateUp {
			availableNum++
		}
	}

	if availableNum < lowerLimit {
		return false
	}

	if cc.Status.Store.StatefulSet == nil || cc.Status.Store.StatefulSet.ReadyReplicas < lowerLimit {
		return false
	}

	return true
}

func (cc *CellCluster) GetClusterID() string {
	return cc.Status.ClusterID
}
