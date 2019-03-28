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

import (
	apps "k8s.io/api/apps/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
)

const (
	// StoreStateUp represents status of Up of Cell Store
	StoreStateUp string = "Up"
	// StoreStateDown represents status of Down of Cell Store
	StoreStateDown string = "Down"
	// StoreStateOffline represents status of Offline of Cell Store
	StoreStateOffline string = "Offline"
	// StoreStateTombstone represents status of Tombstone of Cell Store
	StoreStateTombstone string = "Tombstone"
)

// MemberType represents member type
type MemberType string

const (
	// PDMemberType is pd container type
	PDMemberType MemberType = "pd"
	// StoreMemberType is cell store container type
	StoreMemberType MemberType = "store"
	// ProxyMemberType is redis proxy contianer type
	ProxyMemberType MemberType = "proxy"
	// UnknownMemberType is unknown container type
	UnknownMemberType MemberType = "unknown"
)

// MemberPhase is the current state of member
type MemberPhase string

const (
	// NormalPhase represents normal state of elasticell cluster.
	NormalPhase MemberPhase = "Normal"
	// UpgradePhase represents the upgrade state of elasticell cluster.
	UpgradePhase MemberPhase = "Upgrade"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CellCluster is the control script's spec
type CellCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Spec defines the behavior of a elasticell cluster
	Spec CellClusterSpec `json:"spec"`

	// Most recently observed status of the elasticell cluster
	Status CellClusterStatus `json:"status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CellClusterList is CellCluster list
type CellClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CellCluster `json:"items"`
}

// CellClusterSpec describes the attributes that a user creates on a elasticell cluster
type CellClusterSpec struct {
	SchedulerName string    `json:"schedulerName,omitempty"`
	PD            PDSpec    `json:"pd,omitempty"`
	Proxy         ProxySpec `json:"proxy,omitempty"`
	Store         StoreSpec `json:"store,omitempty"`
	// Services list non-headless services type used in CellCluster
	Services        []Service                            `json:"services,omitempty"`
	PVReclaimPolicy corev1.PersistentVolumeReclaimPolicy `json:"pvReclaimPolicy,omitempty"`
	Timezone        string                               `json:"timezone,omitempty"`
}

// CellClusterStatus represents the current status of a cell cluster.
type CellClusterStatus struct {
	ClusterID string      `json:"clusterID,omitempty"`
	PD        PDStatus    `json:"pd,omitempty"`
	PdPeerURL string      `json:"peerurl,omitempty"`
	Store     StoreStatus `json:"store,omitempty"`
	Proxy     ProxyStatus `json:"proxy,omitempty"`
}

// PDSpec contains details of PD member
type PDSpec struct {
	ContainerSpec
	Replicas             int32               `json:"replicas"`
	NodeSelector         map[string]string   `json:"nodeSelector,omitempty"`
	NodeSelectorRequired bool                `json:"nodeSelectorRequired,omitempty"`
	StorageClassName     string              `json:"storageClassName,omitempty"`
	Tolerations          []corev1.Toleration `json:"tolerations,omitempty"`
}

// ProxySpec contains details of redis proxy member
type ProxySpec struct {
	ContainerSpec
	Replicas             int32               `json:"replicas"`
	NodeSelector         map[string]string   `json:"nodeSelector,omitempty"`
	NodeSelectorRequired bool                `json:"nodeSelectorRequired,omitempty"`
	StorageClassName     string              `json:"storageClassName,omitempty"`
	Tolerations          []corev1.Toleration `json:"tolerations,omitempty"`
}

// StoreSpec contains details of cell store member
type StoreSpec struct {
	ContainerSpec
	Replicas             int32               `json:"replicas"`
	NodeSelector         map[string]string   `json:"nodeSelector,omitempty"`
	NodeSelectorRequired bool                `json:"nodeSelectorRequired,omitempty"`
	StorageClassName     string              `json:"storageClassName,omitempty"`
	Tolerations          []corev1.Toleration `json:"tolerations,omitempty"`
}

// ContainerSpec is the container spec of a pod
type ContainerSpec struct {
	Image           string               `json:"image"`
	ImagePullPolicy corev1.PullPolicy    `json:"imagePullPolicy,omitempty"`
	Requests        *ResourceRequirement `json:"requests,omitempty"`
	Limits          *ResourceRequirement `json:"limits,omitempty"`
}

// Service represent service type used in CellCluster
type Service struct {
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// ResourceRequirement is resource requirements for a pod
type ResourceRequirement struct {
	// CPU is how many cores a pod requires
	CPU string `json:"cpu,omitempty"`
	// Memory is how much memory a pod requires
	Memory string `json:"memory,omitempty"`
	// Storage is storage size a pod requires
	Storage string `json:"storage,omitempty"`
}

// PDStatus is PD status
type PDStatus struct {
	Synced         bool                       `json:"synced,omitempty"`
	Phase          MemberPhase                `json:"phase,omitempty"`
	StatefulSet    *apps.StatefulSetStatus    `json:"statefulSet,omitempty"`
	Members        map[string]PDMember        `json:"members,omitempty"`
	Leader         PDMember                   `json:"leader,omitempty"`
	FailureMembers map[string]PDFailureMember `json:"failureMembers,omitempty"`
}

// PDMember is PD member
type PDMember struct {
	Name string `json:"name"`
	// member id is actually a uint64, but apimachinery's json only treats numbers as int64/float64
	// so uint64 may overflow int64 and thus convert to float64
	ID        string `json:"id"`
	ClientURL string `json:"clientURL"`
	Health    bool   `json:"health"`
	// Last time the health transitioned from one to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// PDFailureMember is the pd failure member information
type PDFailureMember struct {
	PodName       string    `json:"podName,omitempty"`
	MemberID      string    `json:"memberID,omitempty"`
	PVCUID        types.UID `json:"pvcUID,omitempty"`
	MemberDeleted bool      `json:"memberDeleted,omitempty"`
}

// ProxyStatus is proxy status
type ProxyStatus struct {
	Phase          MemberPhase                   `json:"phase,omitempty"`
	StatefulSet    *apps.StatefulSetStatus       `json:"statefulSet,omitempty"`
	Members        map[string]ProxyMember        `json:"members,omitempty"`
	FailureMembers map[string]ProxyFailureMember `json:"failureMembers,omitempty"`
}

// ProxyMember is proxy member
type ProxyMember struct {
	Name   string `json:"name"`
	Health bool   `json:"health"`
	// Last time the health transitioned from one to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// ProxyFailureMember is the cell-proxy failure member information
type ProxyFailureMember struct {
	PodName string `json:"podName,omitempty"`
}

// StoreStatus is store status
type StoreStatus struct {
	Synced          bool                      `json:"synced,omitempty"`
	Phase           MemberPhase               `json:"phase,omitempty"`
	StatefulSet     *apps.StatefulSetStatus   `json:"statefulSet,omitempty"`
	Stores          map[string]KVStore        `json:"stores,omitempty"`
	TombstoneStores map[string]KVStore        `json:"tombstoneStores,omitempty"`
	FailureStores   map[string]KVFailureStore `json:"failureStores,omitempty"`
}

// KVStore is either Up/Down/Offline/Tombstone
type KVStore struct {
	// store id is also uint64, due to the same reason as pd id, we store id as string
	ID                string      `json:"id"`
	PodName           string      `json:"podName"`
	IP                string      `json:"ip"`
	LeaderCount       int32       `json:"leaderCount"`
	State             string      `json:"state"`
	LastHeartbeatTime metav1.Time `json:"lastHeartbeatTime"`
	// Last time the health transitioned from one to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// KVFailureStore is the store failure store information
type KVFailureStore struct {
	PodName string `json:"podName,omitempty"`
	StoreID string `json:"storeID,omitempty"`
}
