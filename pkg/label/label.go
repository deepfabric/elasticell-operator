// Copyright 2017 Deepfabric, Inc.
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
// limitations under the License.package spec

package label

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	// The following labels are recommended by kubernetes https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/

	// ManagedByLabelKey is Kubernetes recommended label key, it represents the tool being used to manage the operation of an application
	// For resources managed by Cell Operator, its value is always cell-operator
	ManagedByLabelKey string = "app.kubernetes.io/managed-by"
	// ComponentLabelKey is Kubernetes recommended label key, it represents the component within the architecture
	ComponentLabelKey string = "app.kubernetes.io/component"
	// NameLabelKey is Kubernetes recommended label key, it represents the name of the application
	// It should always be cell-cluster in our case.
	NameLabelKey string = "app.kubernetes.io/name"
	// InstanceLabelKey is Kubernetes recommended label key, it represents a unique name identifying the instance of an application
	// It's set by helm when installing a release
	InstanceLabelKey string = "app.kubernetes.io/instance"

	// NamespaceLabelKey is label key used in PV for easy querying
	NamespaceLabelKey string = "app.kubernetes.io/namespace"

	// ClusterIDLabelKey is cluster id label key
	ClusterIDLabelKey string = "cell.deepfabric.com/cluster-id"
	// StoreIDLabelKey is store id label key
	StoreIDLabelKey string = "cell.deepfabric.com/store-id"
	// MemberIDLabelKey is member id label key
	MemberIDLabelKey string = "cell.deepfabric.com/member-id"
	// AnnPodNameKey is pod name annotation key used in PV/PVC for synchronizing cell cluster meta info
	AnnPodNameKey string = "cell.deepfabric.com/pod-name"
	// AnnPVCDeferDeleting is pvc defer deletion annotation key used in PVC for defer deleting PVC
	AnnPVCDeferDeleting = "cell.deepfabric.com/pvc-defer-deleting"
	// AnnPVCPodScheduling is pod scheduling annotation key, it represents whether the pod is scheduling
	AnnPVCPodScheduling = "cell.deepfabric.com/pod-scheduling"

	// PDLabelVal is PD label value
	PDLabelVal string = "pd"
	// ProxyLabelVal is Proxy label value
	ProxyLabelVal string = "proxy"
	// StoreLabelVal is Store label value
	StoreLabelVal string = "store"
)

// Label is the label field in metadata
type Label map[string]string

// New initialize a new Label
func New() Label {
	return Label{
		NameLabelKey:      "cell-cluster",
		ManagedByLabelKey: "cell-operator",
	}
}

// Instance adds instance kv pair to label
func (l Label) Instance(name string) Label {
	l[InstanceLabelKey] = name
	return l
}

// Namespace adds namespace kv pair to label
func (l Label) Namespace(name string) Label {
	l[NamespaceLabelKey] = name
	return l
}

// Component adds component kv pair to label
func (l Label) Component(name string) Label {
	l[ComponentLabelKey] = name
	return l
}

// ComponentType returns component type
func (l Label) ComponentType() string {
	return l[ComponentLabelKey]
}

// PD assigns pd to component key in label
func (l Label) PD() Label {
	l.Component(PDLabelVal)
	return l
}

// IsPD returns whether label is a PD
func (l Label) IsPD() bool {
	return l[ComponentLabelKey] == PDLabelVal
}

// Proxy assigns proxy to component key in label
func (l Label) Proxy() Label {
	l.Component(ProxyLabelVal)
	return l
}

// Store assigns store to component key in label
func (l Label) Store() Label {
	l.Component(StoreLabelVal)
	return l
}

// IsStore returns whether label is a Store
func (l Label) IsStore() bool {
	return l[ComponentLabelKey] == StoreLabelVal
}

// IsProxy returns whether label is a Proxy
func (l Label) IsProxy() bool {
	return l[ComponentLabelKey] == ProxyLabelVal
}

// Selector gets labels.Selector from label
func (l Label) Selector() (labels.Selector, error) {
	return metav1.LabelSelectorAsSelector(l.LabelSelector())
}

// LabelSelector gets LabelSelector from label
func (l Label) LabelSelector() *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: l}
}

// Labels converts label to map[string]string
func (l Label) Labels() map[string]string {
	return l
}
