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
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/deepfabric/elasticell-operator/pkg/label"
	"github.com/deepfabric/elasticell-operator/pkg/manager"
	"github.com/deepfabric/elasticell-operator/pkg/util"
	metapb "github.com/deepfabric/elasticell/pkg/pb/metapb"
	"github.com/deepfabric/elasticell/pkg/pdapi"
	"github.com/golang/glog"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/listers/apps/v1beta1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

// storeMemberManager implements manager.Manager.
type storeMemberManager struct {
	setControl                    controller.StatefulSetControlInterface
	svcControl                    controller.ServiceControlInterface
	pdControl                     controller.PDControlInterface
	setLister                     v1beta1.StatefulSetLister
	svcLister                     corelisters.ServiceLister
	podLister                     corelisters.PodLister
	nodeLister                    corelisters.NodeLister
	autoFailover                  bool
	storeFailover                 Failover
	storeScaler                   Scaler
	storeUpgrader                 Upgrader
	storeStatefulSetIsUpgradingFn func(corelisters.PodLister, controller.PDControlInterface, *apps.StatefulSet, *v1alpha1.CellCluster) (bool, error)
}

// NewStoreMemberManager returns a *storeMemberManager
func NewStoreMemberManager(pdControl controller.PDControlInterface,
	setControl controller.StatefulSetControlInterface,
	svcControl controller.ServiceControlInterface,
	setLister v1beta1.StatefulSetLister,
	svcLister corelisters.ServiceLister,
	podLister corelisters.PodLister,
	nodeLister corelisters.NodeLister,
	autoFailover bool,
	storeFailover Failover,
	storeScaler Scaler,
	storeUpgrader Upgrader) manager.Manager {
	kvmm := storeMemberManager{
		pdControl:     pdControl,
		podLister:     podLister,
		nodeLister:    nodeLister,
		setControl:    setControl,
		svcControl:    svcControl,
		setLister:     setLister,
		svcLister:     svcLister,
		autoFailover:  autoFailover,
		storeFailover: storeFailover,
		storeScaler:   storeScaler,
		storeUpgrader: storeUpgrader,
	}
	kvmm.storeStatefulSetIsUpgradingFn = storeStatefulSetIsUpgrading
	return &kvmm
}

// Sync fulfills the manager.Manager interface
func (stmm *storeMemberManager) Sync(cc *v1alpha1.CellCluster) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()

	if !cc.PDIsAvailable() {
		return controller.RequeueErrorf("CellCluster: [%s/%s], waiting for PD cluster running", ns, ccName)
	}

	return stmm.syncStatefulSetForCellCluster(cc)
}

func (stmm *storeMemberManager) syncStatefulSetForCellCluster(cc *v1alpha1.CellCluster) error {
	ns := cc.GetNamespace()
	ccName := cc.GetName()

	newSet, err := stmm.getNewSetForCellCluster(cc)
	if err != nil {
		return err
	}

	oldSet, err := stmm.setLister.StatefulSets(ns).Get(controller.StoreMemberName(ccName))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	if errors.IsNotFound(err) {
		err = SetLastAppliedConfigAnnotation(newSet)
		if err != nil {
			return err
		}
		err = stmm.setControl.CreateStatefulSet(cc, newSet)
		if err != nil {
			return err
		}
		cc.Status.Store.StatefulSet = &apps.StatefulSetStatus{}
		return nil
	}

	if err := stmm.syncCellClusterStatus(cc, oldSet); err != nil {
		return err
	}

	if _, err := stmm.setStoreLabelsForStore(cc); err != nil {
		return err
	}

	if !templateEqual(newSet.Spec.Template, oldSet.Spec.Template) || cc.Status.Store.Phase == v1alpha1.UpgradePhase {
		if err := stmm.storeUpgrader.Upgrade(cc, oldSet, newSet); err != nil {
			return err
		}
	}

	if *newSet.Spec.Replicas > *oldSet.Spec.Replicas {
		if err := stmm.storeScaler.ScaleOut(cc, oldSet, newSet); err != nil {
			return err
		}
	}

	if *newSet.Spec.Replicas < *oldSet.Spec.Replicas {
		if err := stmm.storeScaler.ScaleIn(cc, oldSet, newSet); err != nil {
			return err
		}
	}

	if stmm.autoFailover {
		if cc.StoreAllPodsStarted() && !cc.StoreAllStoresReady() {
			if err := stmm.storeFailover.Failover(cc); err != nil {
				return err
			}
		}
	}

	if !statefulSetEqual(*newSet, *oldSet) {
		set := *oldSet
		set.Spec.Template = newSet.Spec.Template
		*set.Spec.Replicas = *newSet.Spec.Replicas
		set.Spec.UpdateStrategy = newSet.Spec.UpdateStrategy
		err := SetLastAppliedConfigAnnotation(&set)
		if err != nil {
			return err
		}
		_, err = stmm.setControl.UpdateStatefulSet(cc, &set)
		return err
	}

	return nil
}

func (stmm *storeMemberManager) getNewSetForCellCluster(cc *v1alpha1.CellCluster) (*apps.StatefulSet, error) {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	storeConfigMap := controller.StoreMemberName(ccName)
	annMount, annVolume := annotationsMountVolume()
	volMounts := []corev1.VolumeMount{
		annMount,
		{Name: v1alpha1.StoreMemberType.String(), MountPath: "/var/lib/store"},
		{Name: "config", ReadOnly: true, MountPath: "/etc/store"},
		{Name: "startup-script", ReadOnly: true, MountPath: "/usr/local/bin"},
	}
	vols := []corev1.Volume{
		annVolume,
		{Name: "config", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: storeConfigMap,
				},
				Items: []corev1.KeyToPath{{Key: "config-file", Path: "store.toml"}},
			}},
		},
		{Name: "startup-script", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: storeConfigMap,
				},
				Items: []corev1.KeyToPath{{Key: "startup-script", Path: "store_start_script.sh"}},
			}},
		},
	}

	var q resource.Quantity
	var err error

	if cc.Spec.Store.Requests != nil {
		size := cc.Spec.Store.Requests.Storage
		q, err = resource.ParseQuantity(size)
		if err != nil {
			return nil, fmt.Errorf("cant' get storage size: %s for CellCluster: %s/%s, %v", size, ns, ccName, err)
		}
	}

	storeLabel := stmm.labelStore(cc)
	setName := controller.StoreMemberName(ccName)
	capacity := controller.StoreCapacity(cc.Spec.Store.Limits)
	storageClassName := cc.Spec.Store.StorageClassName
	if storageClassName == "" {
		storageClassName = controller.DefaultStorageClassName
	}

	storeset := &apps.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            setName,
			Namespace:       ns,
			Labels:          storeLabel.Labels(),
			OwnerReferences: []metav1.OwnerReference{controller.GetOwnerRef(cc)},
		},
		Spec: apps.StatefulSetSpec{
			Replicas: func() *int32 { r := cc.StoreRealReplicas(); return &r }(),
			Selector: storeLabel.LabelSelector(),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: storeLabel.Labels(),
				},
				Spec: corev1.PodSpec{
					SchedulerName: cc.Spec.SchedulerName,
					Affinity: util.AffinityForNodeSelector(
						ns,
						cc.Spec.Store.NodeSelectorRequired,
						storeLabel,
						cc.Spec.Store.NodeSelector,
					),
					Containers: []corev1.Container{
						{
							Name:            v1alpha1.StoreMemberType.String(),
							Image:           cc.Spec.Store.Image,
							Command:         []string{"/bin/sh", "/usr/local/bin/store_start_script.sh"},
							ImagePullPolicy: cc.Spec.Store.ImagePullPolicy,
							Ports: []corev1.ContainerPort{
								{
									Name:          "server",
									ContainerPort: int32(20160),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: volMounts,
							Resources:    util.ResourceRequirement(cc.Spec.Store.ContainerSpec),
							Env: []corev1.EnvVar{
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
									Value: ccName,
								},
								{
									Name:  "CAPACITY",
									Value: capacity,
								},
								{
									Name:  "TZ",
									Value: cc.Spec.Timezone,
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyAlways,
					Tolerations:   cc.Spec.Store.Tolerations,
					Volumes:       vols,
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				stmm.volumeClaimTemplate(q, v1alpha1.StoreMemberType.String(), &storageClassName),
			},
			PodManagementPolicy: apps.ParallelPodManagement,
			UpdateStrategy: apps.StatefulSetUpdateStrategy{
				Type: apps.RollingUpdateStatefulSetStrategyType,
				RollingUpdate: &apps.RollingUpdateStatefulSetStrategy{
					Partition: func() *int32 { r := cc.StoreRealReplicas(); return &r }(),
				},
			},
		},
	}
	return storeset, nil
}

func (stmm *storeMemberManager) volumeClaimTemplate(q resource.Quantity, metaName string, storageClassName *string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: metaName},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			StorageClassName: storageClassName,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: q,
				},
			},
		},
	}
}

func (stmm *storeMemberManager) labelStore(cc *v1alpha1.CellCluster) label.Label {
	instanceName := cc.GetLabels()[label.InstanceLabelKey]
	return label.New().Instance(instanceName).Store()
}

func (stmm *storeMemberManager) syncCellClusterStatus(cc *v1alpha1.CellCluster, set *apps.StatefulSet) error {
	cc.Status.Store.StatefulSet = &set.Status
	upgrading, err := stmm.storeStatefulSetIsUpgradingFn(stmm.podLister, stmm.pdControl, set, cc)
	if err != nil {
		return err
	}
	if upgrading && cc.Status.PD.Phase != v1alpha1.UpgradePhase {
		cc.Status.Store.Phase = v1alpha1.UpgradePhase
	} else {
		cc.Status.Store.Phase = v1alpha1.NormalPhase
	}

	previousStores := cc.Status.Store.Stores
	stores := map[string]v1alpha1.KVStore{}
	tombstoneStores := map[string]v1alpha1.KVStore{}

	pdCli := stmm.pdControl.GetPDClient(cc)
	// This only returns Up/Down/Offline stores
	storeInfo, err := pdCli.GetStores()
	if err != nil {
		cc.Status.Store.Synced = false
		return err
	}

	for _, store := range storeInfo.Stores {
		status := stmm.getKVStore(store)
		if status == nil {
			continue
		}
		// avoid LastHeartbeatTime be overwrite by zero time when pd lost LastHeartbeatTime
		if status.LastHeartbeatTime.IsZero() {
			if oldStatus, ok := previousStores[status.ID]; ok {
				glog.V(4).Infof("the pod:%s's store LastHeartbeatTime is zero,so will keep in %v", status.PodName, oldStatus.LastHeartbeatTime)
				status.LastHeartbeatTime = oldStatus.LastHeartbeatTime
			}
		}

		oldStore, exist := previousStores[status.ID]
		if exist {
			status.LastTransitionTime = oldStore.LastTransitionTime
		}
		if !exist || status.State != oldStore.State {
			status.LastTransitionTime = metav1.Now()
		}

		stores[status.ID] = *status
	}

	//this returns all tombstone stores
	tombstoneStoresInfo, err := pdCli.GetTombStoneStores()
	if err != nil {
		cc.Status.Store.Synced = false
		return err
	}
	for _, store := range tombstoneStoresInfo.Stores {
		status := stmm.getKVStore(store)
		if status == nil {
			continue
		}
		tombstoneStores[status.ID] = *status
	}

	cc.Status.Store.Synced = true
	cc.Status.Store.Stores = stores
	cc.Status.Store.TombstoneStores = tombstoneStores
	return nil
}

func (stmm *storeMemberManager) getKVStore(store *pdapi.StoreInfo) *v1alpha1.KVStore {
	if store == nil || store.Status == nil {
		return nil
	}
	storeID := fmt.Sprintf("%d", store.Meta.ID)
	ip := strings.Split(store.Meta.Address, ":")[0]
	// to do the same podname?
	podName := strings.Split(ip, ".")[0]

	heartBeatTime := time.Time{}
	if store.Status.LastHeartbeatTS > 0 {
		heartBeatTime = time.Unix(store.Status.LastHeartbeatTS, 0)
	}

	return &v1alpha1.KVStore{
		ID:                storeID,
		PodName:           podName,
		IP:                ip,
		LeaderCount:       int32(store.Status.LeaderCount),
		State:             strconv.Itoa(int(store.Meta.State)),
		LastHeartbeatTime: metav1.Time{Time: heartBeatTime},
	}
}

func (stmm *storeMemberManager) setStoreLabelsForStore(cc *v1alpha1.CellCluster) (int, error) {
	ns := cc.GetNamespace()
	// for unit test
	setCount := 0

	pdCli := stmm.pdControl.GetPDClient(cc)
	storesInfo, err := pdCli.GetStores()
	if err != nil {
		return setCount, err
	}

	for _, store := range storesInfo.Stores {
		status := stmm.getKVStore(store)
		if status == nil {
			continue
		}
		podName := status.PodName

		pod, err := stmm.podLister.Pods(ns).Get(podName)
		if err != nil {
			return setCount, err
		}

		nodeName := pod.Spec.NodeName
		ls, err := stmm.getNodeLabels(nodeName)
		if err != nil {
			glog.Warningf("node: [%s] has no node labels, skipping set store labels for Pod: [%s/%s]", nodeName, ns, podName)
			continue
		}
		if !stmm.storeLabelsEqualNodeLabels(store.Meta.Lables, ls) {
			set, err := pdCli.SetStoreLabels(store.Meta.ID, ls)
			if err != nil {
				glog.Warningf("failed to set pod: [%s/%s]'s store labels: %v", ns, podName, ls)
				continue
			}
			if set {
				setCount++
				glog.Infof("pod: [%s/%s] set labels: %v successfully", ns, podName, ls)
			}
		}
	}

	return setCount, nil
}

func (stmm *storeMemberManager) getNodeLabels(nodeName string) (map[string]string, error) {
	node, err := stmm.nodeLister.Get(nodeName)
	if err != nil {
		return nil, err
	}
	if ls := node.GetLabels(); ls != nil {
		labels := map[string]string{}
		if region, found := ls["region"]; found {
			labels["region"] = region
		}
		if zone, found := ls["zone"]; found {
			labels["zone"] = zone
		}
		if rack, found := ls["rack"]; found {
			labels["rack"] = rack
		}
		if host, found := ls[apis.LabelHostname]; found {
			labels["host"] = host
		}
		return labels, nil
	}
	return nil, fmt.Errorf("labels not found")
}

// storeLabelsEqualNodeLabels compares store labels with node labels
// for historic reasons, PD stores Store labels as []*StoreLabel which is a key-value pair slice
func (stmm *storeMemberManager) storeLabelsEqualNodeLabels(storeLabels []metapb.Label, nodeLabels map[string]string) bool {
	ls := map[string]string{}
	for _, label := range storeLabels {
		key := label.GetKey()
		if _, ok := nodeLabels[key]; ok {
			val := label.GetValue()
			ls[key] = val
		}
	}
	return reflect.DeepEqual(ls, nodeLabels)
}

func storeStatefulSetIsUpgrading(podLister corelisters.PodLister, pdControl controller.PDControlInterface, set *apps.StatefulSet, cc *v1alpha1.CellCluster) (bool, error) {
	if statefulSetIsUpgrading(set) {
		return true, nil
	}
	instanceName := cc.GetLabels()[label.InstanceLabelKey]
	selector, err := label.New().Instance(instanceName).Store().Selector()
	if err != nil {
		return false, err
	}
	storePods, err := podLister.Pods(cc.GetNamespace()).List(selector)
	if err != nil {
		return false, err
	}
	for _, pod := range storePods {
		revisionHash, exist := pod.Labels[apps.ControllerRevisionHashLabelKey]
		if !exist {
			return false, nil
		}
		if revisionHash != cc.Status.Store.StatefulSet.UpdateRevision {
			return true, nil
		}
	}
	return false, nil
	/*
		evictLeaderSchedulers, err := pdControl.GetPDClient(cc).GetEvictLeaderSchedulers()
		if err != nil {
			return false, err
		}

		return evictLeaderSchedulers != nil && len(evictLeaderSchedulers) > 0, nil
	*/
}

type FakeStoreMemberManager struct {
	err error
}

func NewFakeStoreMemberManager() *FakeStoreMemberManager {
	return &FakeStoreMemberManager{}
}

func (ftmm *FakeStoreMemberManager) SetSyncError(err error) {
	ftmm.err = err
}

func (ftmm *FakeStoreMemberManager) Sync(_ *v1alpha1.CellCluster) error {
	if ftmm.err != nil {
		return ftmm.err
	}
	return nil
}
