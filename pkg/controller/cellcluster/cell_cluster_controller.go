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
	"fmt"
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned"
	informers "github.com/deepfabric/elasticell-operator/pkg/client/informers/externalversions"
	listers "github.com/deepfabric/elasticell-operator/pkg/client/listers/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	mm "github.com/deepfabric/elasticell-operator/pkg/manager/member"
	"github.com/deepfabric/elasticell-operator/pkg/manager/meta"
	"github.com/golang/glog"
	perrors "github.com/pingcap/errors"
	apps "k8s.io/api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	eventv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("CellCluster")

// Controller controls cellclusters.
type Controller struct {
	// kubernetes client interface
	kubeClient kubernetes.Interface
	// operator client interface
	cli versioned.Interface
	// control returns an interface capable of syncing a cell cluster.
	// Abstracted out for testing.
	control ControlInterface
	// ccLister is able to list/get cellclusters from a shared informer's store
	ccLister listers.CellClusterLister
	// ccListerSynced returns true if the cellcluster shared informer has synced at least once
	ccListerSynced cache.InformerSynced
	// setLister is able to list/get stateful sets from a shared informer's store
	setLister appslisters.StatefulSetLister
	// setListerSynced returns true if the statefulset shared informer has synced at least once
	setListerSynced cache.InformerSynced
	// cellclusters that need to be synced.
	queue workqueue.RateLimitingInterface
}

// NewController creates a cellcluster controller.
func NewController(
	kubeCli kubernetes.Interface,
	cli versioned.Interface,
	informerFactory informers.SharedInformerFactory,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
) *Controller {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&eventv1.EventSinkImpl{
		Interface: eventv1.New(kubeCli.CoreV1().RESTClient()).Events("")})
	recorder := eventBroadcaster.NewRecorder(v1alpha1.Scheme, corev1.EventSource{Component: "cellcluster"})

	tcInformer := informerFactory.Deepfabric().V1alpha1().CellClusters()
	setInformer := kubeInformerFactory.Apps().V1beta1().StatefulSets()
	svcInformer := kubeInformerFactory.Core().V1().Services()
	pvcInformer := kubeInformerFactory.Core().V1().PersistentVolumeClaims()
	pvInformer := kubeInformerFactory.Core().V1().PersistentVolumes()
	podInformer := kubeInformerFactory.Core().V1().Pods()
	nodeInformer := kubeInformerFactory.Core().V1().Nodes()

	ccControl := controller.NewRealCellClusterControl(cli, tcInformer.Lister(), recorder)
	pdControl := controller.NewDefaultPDControl()
	proxyControl := controller.NewDefaultProxyControl()
	setControl := controller.NewRealStatefuSetControl(kubeCli, setInformer.Lister(), recorder)
	svcControl := controller.NewRealServiceControl(kubeCli, svcInformer.Lister(), recorder)
	pvControl := controller.NewRealPVControl(kubeCli, pvcInformer.Lister(), pvInformer.Lister(), recorder)
	pvcControl := controller.NewRealPVCControl(kubeCli, recorder, pvcInformer.Lister())
	podControl := controller.NewRealPodControl(kubeCli, pdControl, podInformer.Lister(), recorder)
	storeScaler := mm.NewStoreScaler(pdControl, pvcInformer.Lister(), pvcControl, podInformer.Lister())
	storeFailover := mm.NewStoreFailover(pdControl)
	pdUpgrader := mm.NewPDUpgrader(pdControl, podControl, podInformer.Lister())
	storeUpgrader := mm.NewStoreUpgrader(pdControl, podControl, podInformer.Lister())
	proxyUpgrader := mm.NewProxyUpgrader(proxyControl, podInformer.Lister())

	ccc := &Controller{
		kubeClient: kubeCli,
		cli:        cli,
		control: NewDefaultCellClusterControl(
			ccControl,
			mm.NewPDMemberManager(
				pdControl,
				setControl,
				svcControl,
				setInformer.Lister(),
				svcInformer.Lister(),
				podInformer.Lister(),
				podControl,
				pvcInformer.Lister(),
				pdUpgrader,
			),
			mm.NewStoreMemberManager(
				pdControl,
				setControl,
				svcControl,
				setInformer.Lister(),
				svcInformer.Lister(),
				podInformer.Lister(),
				nodeInformer.Lister(),
				false,
				storeFailover,
				storeScaler,
				storeUpgrader,
			),
			mm.NewProxyMemberManager(
				setControl,
				svcControl,
				proxyControl,
				setInformer.Lister(),
				svcInformer.Lister(),
				podInformer.Lister(),
				proxyUpgrader,
			),
			meta.NewReclaimPolicyManager(
				pvcInformer.Lister(),
				pvInformer.Lister(),
				pvControl,
			),
			meta.NewMetaManager(
				pvcInformer.Lister(),
				pvcControl,
				pvInformer.Lister(),
				pvControl,
				podInformer.Lister(),
				podControl,
			),
			mm.NewOrphanPodsCleaner(
				podInformer.Lister(),
				podControl,
				pvcInformer.Lister(),
			),
			recorder,
		),
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"cellcluster",
		),
	}

	tcInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: ccc.enqueueCellCluster,
		UpdateFunc: func(old, cur interface{}) {
			ccc.enqueueCellCluster(cur)
		},
		DeleteFunc: ccc.enqueueCellCluster,
	})
	ccc.ccLister = tcInformer.Lister()
	ccc.ccListerSynced = tcInformer.Informer().HasSynced

	setInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: ccc.addStatefulSet,
		UpdateFunc: func(old, cur interface{}) {
			ccc.updateStatefuSet(old, cur)
		},
		DeleteFunc: ccc.deleteStatefulSet,
	})
	ccc.setLister = setInformer.Lister()
	ccc.setListerSynced = setInformer.Informer().HasSynced

	return ccc
}

// Run runs the cellcluster controller.
func (ccc *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer ccc.queue.ShutDown()

	glog.Info("Starting cellcluster controller")
	defer glog.Info("Shutting down cellcluster controller")

	if !cache.WaitForCacheSync(stopCh, ccc.ccListerSynced, ccc.setListerSynced) {
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(ccc.worker, time.Second, stopCh)
	}

	<-stopCh
}

// worker runs a worker goroutine that invokes processNextWorkItem until the the controller's queue is closed
func (ccc *Controller) worker() {
	for ccc.processNextWorkItem() {
		// revive:disable:empty-block
		glog.V(2).Infof("process one item")
	}
	glog.V(2).Infof("worker over")
}

// processNextWorkItem dequeues items, processes them, and marks them done. It enforces that the syncHandler is never
// invoked concurrently with the same key.
func (ccc *Controller) processNextWorkItem() bool {
	key, quit := ccc.queue.Get()
	if quit {
		return false
	}
	defer ccc.queue.Done(key)
	glog.V(2).Infof("do sync")
	if err := ccc.sync(key.(string)); err != nil {
		if perrors.Find(err, controller.IsRequeueError) != nil {
			glog.Infof("CellCluster: %v, still need sync: %v, requeuing", key.(string), err)
		} else {
			utilruntime.HandleError(fmt.Errorf("CellCluster: %v, sync failed %v, requeuing", key.(string), err))
		}
		ccc.queue.AddRateLimited(key)
	} else {
		ccc.queue.Forget(key)
	}
	return true
}

// sync syncs the given cellcluster.
func (ccc *Controller) sync(key string) error {
	startTime := time.Now()
	defer func() {
		glog.V(4).Infof("Finished syncing CellCluster %q (%v)", key, time.Since(startTime))
	}()

	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	cc, err := ccc.ccLister.CellClusters(ns).Get(name)
	if errors.IsNotFound(err) {
		glog.Infof("CellCluster has been deleted %v", key)
		return nil
	}
	if err != nil {
		return err
	}

	return ccc.syncCellCluster(cc.DeepCopy())
}

func (ccc *Controller) syncCellCluster(cc *v1alpha1.CellCluster) error {
	return ccc.control.UpdateCellCluster(cc)
}

// enqueueCellCluster enqueues the given cellcluster in the work queue.
func (ccc *Controller) enqueueCellCluster(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Cound't get key for object %+v: %v", obj, err))
		return
	}
	ccc.queue.Add(key)
}

// addStatefulSet adds the cellcluster for the statefulset to the sync queue
func (ccc *Controller) addStatefulSet(obj interface{}) {
	set := obj.(*apps.StatefulSet)
	ns := set.GetNamespace()
	setName := set.GetName()

	if set.DeletionTimestamp != nil {
		// on a restart of the controller manager, it's possible a new statefulset shows up in a state that
		// is already pending deletion. Prevent the statefulset from being a creation observation.
		ccc.deleteStatefulSet(set)
		return
	}

	// If it has a ControllerRef, that's all that matters.
	cc := ccc.resolveCellClusterFromSet(ns, set)
	if cc == nil {
		return
	}
	glog.V(4).Infof("StatefuSet %s/%s created, CellCluster: %s/%s", ns, setName, ns, cc.Name)
	ccc.enqueueCellCluster(cc)
}

// updateStatefuSet adds the cellcluster for the current and old statefulsets to the sync queue.
func (ccc *Controller) updateStatefuSet(old, cur interface{}) {
	curSet := cur.(*apps.StatefulSet)
	oldSet := old.(*apps.StatefulSet)
	ns := curSet.GetNamespace()
	setName := curSet.GetName()
	if curSet.ResourceVersion == oldSet.ResourceVersion {
		// Periodic resync will send update events for all known statefulsets.
		// Two different versions of the same statefulset will always have different RVs.
		return
	}

	// If it has a ControllerRef, that's all that matters.
	cc := ccc.resolveCellClusterFromSet(ns, curSet)
	if cc == nil {
		return
	}
	glog.V(4).Infof("StatefulSet %s/%s updated, %+v -> %+v.", ns, setName, oldSet.Spec, curSet.Spec)
	ccc.enqueueCellCluster(cc)
}

// deleteStatefulSet enqueues the cellcluster for the statefulset accounting for deletion tombstones.
func (ccc *Controller) deleteStatefulSet(obj interface{}) {
	set, ok := obj.(*apps.StatefulSet)
	ns := set.GetNamespace()
	setName := set.GetName()

	// When a delete is dropped, the relist will notice a statefuset in the store not
	// in the list, leading to the insertion of a tombstone object which contains
	// the deleted key/value.
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %+v", obj))
			return
		}
		set, ok = tombstone.Obj.(*apps.StatefulSet)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a statefuset %+v", obj))
			return
		}
	}

	// If it has a CellCluster, that's all that matters.
	cc := ccc.resolveCellClusterFromSet(ns, set)
	if cc == nil {
		return
	}
	glog.V(4).Infof("StatefulSet %s/%s deleted through %v.", ns, setName, utilruntime.GetCaller())
	ccc.enqueueCellCluster(cc)
}

// resolveCellClusterFromSet returns the CellCluster by a StatefulSet,
// or nil if the StatefulSet could not be resolved to a matching CellCluster
// of the correct Kind.
func (ccc *Controller) resolveCellClusterFromSet(namespace string, set *apps.StatefulSet) *v1alpha1.CellCluster {
	controllerRef := metav1.GetControllerOf(set)
	if controllerRef == nil {
		return nil
	}

	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != controllerKind.Kind {
		return nil
	}
	cc, err := ccc.ccLister.CellClusters(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if cc.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return cc
}
