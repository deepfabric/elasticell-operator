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

package controller

import (
	"fmt"
	"strings"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned"
	ccinformers "github.com/deepfabric/elasticell-operator/pkg/client/informers/externalversions/deepfabric.com/v1alpha1"
	listers "github.com/deepfabric/elasticell-operator/pkg/client/listers/deepfabric.com/v1alpha1"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

// CellClusterControlInterface manages CellClusters
type CellClusterControlInterface interface {
	UpdateCellCluster(*v1alpha1.CellCluster, *v1alpha1.CellClusterStatus, *v1alpha1.CellClusterStatus) (*v1alpha1.CellCluster, error)
}

type realCellClusterControl struct {
	cli      versioned.Interface
	ccLister listers.CellClusterLister
	recorder record.EventRecorder
}

// NewRealCellClusterControl creates a new CellClusterControlInterface
func NewRealCellClusterControl(cli versioned.Interface,
	ccLister listers.CellClusterLister,
	recorder record.EventRecorder) CellClusterControlInterface {
	return &realCellClusterControl{
		cli,
		ccLister,
		recorder,
	}
}

func (rtc *realCellClusterControl) UpdateCellCluster(cc *v1alpha1.CellCluster, newStatus *v1alpha1.CellClusterStatus, oldStatus *v1alpha1.CellClusterStatus) (*v1alpha1.CellCluster, error) {
	ns := cc.GetNamespace()
	ccName := cc.GetName()

	status := cc.Status.DeepCopy()
	var updateCC *v1alpha1.CellCluster

	// don't wait due to limited number of clients, but backoff after the default number of steps
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var updateErr error
		updateCC, updateErr = rtc.cli.DeepfabricV1alpha1().CellClusters(ns).Update(cc)
		if updateErr == nil {
			glog.Infof("CellCluster: [%s/%s] updated successfully", ns, ccName)
			return nil
		}
		glog.Errorf("failed to update CellCluster: [%s/%s], error: %v", ns, ccName, updateErr)

		if updated, err := rtc.ccLister.CellClusters(ns).Get(ccName); err == nil {
			// make a copy so we don't mutate the shared cache
			cc = updated.DeepCopy()
			cc.Status = *status
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated CellCluster %s/%s from lister: %v", ns, ccName, err))
		}

		return updateErr
	})
	if !deepEqualExceptHeartbeatTime(newStatus.DeepCopy(), oldStatus.DeepCopy()) {
		rtc.recordCellClusterEvent("update", cc, err)
	}
	return updateCC, err
}

func (rtc *realCellClusterControl) recordCellClusterEvent(verb string, cc *v1alpha1.CellCluster, err error) {
	ccName := cc.GetName()
	if err == nil {
		reason := fmt.Sprintf("Successful%s", strings.Title(verb))
		msg := fmt.Sprintf("%s CellCluster %s successful",
			strings.ToLower(verb), ccName)
		rtc.recorder.Event(cc, corev1.EventTypeNormal, reason, msg)
	} else {
		reason := fmt.Sprintf("Failed%s", strings.Title(verb))
		msg := fmt.Sprintf("%s CellCluster %s failed error: %s",
			strings.ToLower(verb), ccName, err)
		rtc.recorder.Event(cc, corev1.EventTypeWarning, reason, msg)
	}
}

func deepEqualExceptHeartbeatTime(newStatus *v1alpha1.CellClusterStatus, oldStatus *v1alpha1.CellClusterStatus) bool {
	sweepHeartbeatTime(newStatus.Store.Stores)
	sweepHeartbeatTime(newStatus.Store.TombstoneStores)
	sweepHeartbeatTime(oldStatus.Store.Stores)
	sweepHeartbeatTime(oldStatus.Store.TombstoneStores)

	return apiequality.Semantic.DeepEqual(newStatus, oldStatus)
}

func sweepHeartbeatTime(stores map[string]v1alpha1.KVStore) {
	for id, store := range stores {
		store.LastHeartbeatTime = metav1.Time{}
		stores[id] = store
	}
}

// FakeCellClusterControl is a fake CellClusterControlInterface
type FakeCellClusterControl struct {
	TcLister                 listers.CellClusterLister
	TcIndexer                cache.Indexer
	updateCellClusterTracker requestTracker
}

// NewFakeCellClusterControl returns a FakeCellClusterControl
func NewFakeCellClusterControl(tcInformer ccinformers.CellClusterInformer) *FakeCellClusterControl {
	return &FakeCellClusterControl{
		tcInformer.Lister(),
		tcInformer.Informer().GetIndexer(),
		requestTracker{0, nil, 0},
	}
}

// SetUpdateCellClusterError sets the error attributes of updateCellClusterTracker
func (ssc *FakeCellClusterControl) SetUpdateCellClusterError(err error, after int) {
	ssc.updateCellClusterTracker.err = err
	ssc.updateCellClusterTracker.after = after
}

// UpdateCellCluster updates the CellCluster
func (ssc *FakeCellClusterControl) UpdateCellCluster(cc *v1alpha1.CellCluster, _ *v1alpha1.CellClusterStatus, _ *v1alpha1.CellClusterStatus) (*v1alpha1.CellCluster, error) {
	defer ssc.updateCellClusterTracker.inc()
	if ssc.updateCellClusterTracker.errorReady() {
		defer ssc.updateCellClusterTracker.reset()
		return cc, ssc.updateCellClusterTracker.err
	}

	return cc, ssc.TcIndexer.Update(cc)
}
