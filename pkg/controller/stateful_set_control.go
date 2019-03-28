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

	"github.com/golang/glog"
	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	ccinformers "github.com/deepfabric/elasticell-operator/pkg/client/informers/externalversions/deepfabric.com/v1alpha1"
	v1listers "github.com/deepfabric/elasticell-operator/pkg/client/listers/deepfabric.com/v1alpha1"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	appsinformers "k8s.io/client-go/informers/apps/v1beta1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
)

// StatefulSetControlInterface defines the interface that uses to create, update, and delete StatefulSets,
type StatefulSetControlInterface interface {
	// CreateStatefulSet creates a StatefulSet in a CellCluster.
	CreateStatefulSet(*v1alpha1.CellCluster, *apps.StatefulSet) error
	// UpdateStatefulSet updates a StatefulSet in a CellCluster.
	UpdateStatefulSet(*v1alpha1.CellCluster, *apps.StatefulSet) (*apps.StatefulSet, error)
	// DeleteStatefulSet deletes a StatefulSet in a CellCluster.
	DeleteStatefulSet(*v1alpha1.CellCluster, *apps.StatefulSet) error
}

type realStatefulSetControl struct {
	kubeCli   kubernetes.Interface
	setLister appslisters.StatefulSetLister
	recorder  record.EventRecorder
}

// NewRealStatefuSetControl returns a StatefulSetControlInterface
func NewRealStatefuSetControl(kubeCli kubernetes.Interface, setLister appslisters.StatefulSetLister, recorder record.EventRecorder) StatefulSetControlInterface {
	return &realStatefulSetControl{kubeCli, setLister, recorder}
}

// CreateStatefulSet create a StatefulSet in a CellCluster.
func (sc *realStatefulSetControl) CreateStatefulSet(cc *v1alpha1.CellCluster, set *apps.StatefulSet) error {
	_, err := sc.kubeCli.AppsV1beta1().StatefulSets(cc.Namespace).Create(set)
	// sink already exists errors
	if apierrors.IsAlreadyExists(err) {
		return err
	}
	sc.recordStatefulSetEvent("create", cc, set, err)
	return err
}

// UpdateStatefulSet update a StatefulSet in a CellCluster.
func (sc *realStatefulSetControl) UpdateStatefulSet(cc *v1alpha1.CellCluster, set *apps.StatefulSet) (*apps.StatefulSet, error) {
	ns := cc.GetNamespace()
	ccName := cc.GetName()
	setName := set.GetName()
	setSpec := set.Spec.DeepCopy()
	var updatedSS *apps.StatefulSet

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// TODO: verify if StatefulSet identity(name, namespace, labels) matches CellCluster
		var updateErr error
		updatedSS, updateErr = sc.kubeCli.AppsV1beta1().StatefulSets(ns).Update(set)
		if updateErr == nil {
			glog.Infof("CellCluster: [%s/%s]'s StatefulSet: [%s/%s] updated successfully", ns, ccName, ns, setName)
			return nil
		}
		glog.Errorf("failed to update CellCluster: [%s/%s]'s StatefulSet: [%s/%s], error: %v", ns, ccName, ns, setName, updateErr)

		if updated, err := sc.setLister.StatefulSets(ns).Get(setName); err == nil {
			// make a copy so we don't mutate the shared cache
			set = updated.DeepCopy()
			set.Spec = *setSpec
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated StatefulSet %s/%s from lister: %v", ns, setName, err))
		}
		return updateErr
	})

	sc.recordStatefulSetEvent("update", cc, set, err)
	return updatedSS, err
}

// DeleteStatefulSet delete a StatefulSet in a CellCluster.
func (sc *realStatefulSetControl) DeleteStatefulSet(cc *v1alpha1.CellCluster, set *apps.StatefulSet) error {
	err := sc.kubeCli.AppsV1beta1().StatefulSets(cc.Namespace).Delete(set.Name, nil)
	sc.recordStatefulSetEvent("delete", cc, set, err)
	return err
}

func (sc *realStatefulSetControl) recordStatefulSetEvent(verb string, cc *v1alpha1.CellCluster, set *apps.StatefulSet, err error) {
	ccName := cc.Name
	setName := set.Name
	if err == nil {
		reason := fmt.Sprintf("Successful%s", strings.Title(verb))
		message := fmt.Sprintf("%s StatefulSet %s in CellCluster %s successful",
			strings.ToLower(verb), setName, ccName)
		sc.recorder.Event(cc, corev1.EventTypeNormal, reason, message)
	} else {
		reason := fmt.Sprintf("Failed%s", strings.Title(verb))
		message := fmt.Sprintf("%s StatefulSet %s in CellCluster %s failed error: %s",
			strings.ToLower(verb), setName, ccName, err)
		sc.recorder.Event(cc, corev1.EventTypeWarning, reason, message)
	}
}

var _ StatefulSetControlInterface = &realStatefulSetControl{}

// FakeStatefulSetControl is a fake StatefulSetControlInterface
type FakeStatefulSetControl struct {
	SetLister                appslisters.StatefulSetLister
	SetIndexer               cache.Indexer
	TcLister                 v1listers.CellClusterLister
	TcIndexer                cache.Indexer
	createStatefulSetTracker requestTracker
	updateStatefulSetTracker requestTracker
	deleteStatefulSetTracker requestTracker
	statusChange             func(set *apps.StatefulSet)
}

// NewFakeStatefulSetControl returns a FakeStatefulSetControl
func NewFakeStatefulSetControl(setInformer appsinformers.StatefulSetInformer, tcInformer ccinformers.CellClusterInformer) *FakeStatefulSetControl {
	return &FakeStatefulSetControl{
		setInformer.Lister(),
		setInformer.Informer().GetIndexer(),
		tcInformer.Lister(),
		tcInformer.Informer().GetIndexer(),
		requestTracker{0, nil, 0},
		requestTracker{0, nil, 0},
		requestTracker{0, nil, 0},
		nil,
	}
}

// SetCreateStatefulSetError sets the error attributes of createStatefulSetTracker
func (ssc *FakeStatefulSetControl) SetCreateStatefulSetError(err error, after int) {
	ssc.createStatefulSetTracker.err = err
	ssc.createStatefulSetTracker.after = after
}

// SetUpdateStatefulSetError sets the error attributes of updateStatefulSetTracker
func (ssc *FakeStatefulSetControl) SetUpdateStatefulSetError(err error, after int) {
	ssc.updateStatefulSetTracker.err = err
	ssc.updateStatefulSetTracker.after = after
}

// SetDeleteStatefulSetError sets the error attributes of deleteStatefulSetTracker
func (ssc *FakeStatefulSetControl) SetDeleteStatefulSetError(err error, after int) {
	ssc.deleteStatefulSetTracker.err = err
	ssc.deleteStatefulSetTracker.after = after
}

func (ssc *FakeStatefulSetControl) SetStatusChange(fn func(*apps.StatefulSet)) {
	ssc.statusChange = fn
}

// CreateStatefulSet adds the statefulset to SetIndexer
func (ssc *FakeStatefulSetControl) CreateStatefulSet(_ *v1alpha1.CellCluster, set *apps.StatefulSet) error {
	defer func() {
		ssc.createStatefulSetTracker.inc()
		ssc.statusChange = nil
	}()

	if ssc.createStatefulSetTracker.errorReady() {
		defer ssc.createStatefulSetTracker.reset()
		return ssc.createStatefulSetTracker.err
	}

	if ssc.statusChange != nil {
		ssc.statusChange(set)
	}

	return ssc.SetIndexer.Add(set)
}

// UpdateStatefulSet updates the statefulset of SetIndexer
func (ssc *FakeStatefulSetControl) UpdateStatefulSet(_ *v1alpha1.CellCluster, set *apps.StatefulSet) (*apps.StatefulSet, error) {
	defer func() {
		ssc.updateStatefulSetTracker.inc()
		ssc.statusChange = nil
	}()

	if ssc.updateStatefulSetTracker.errorReady() {
		defer ssc.updateStatefulSetTracker.reset()
		return nil, ssc.updateStatefulSetTracker.err
	}

	if ssc.statusChange != nil {
		ssc.statusChange(set)
	}
	return set, ssc.SetIndexer.Update(set)
}

// DeleteStatefulSet deletes the statefulset of SetIndexer
func (ssc *FakeStatefulSetControl) DeleteStatefulSet(_ *v1alpha1.CellCluster, _ *apps.StatefulSet) error {
	return nil
}

var _ StatefulSetControlInterface = &FakeStatefulSetControl{}
