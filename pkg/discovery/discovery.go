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

package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	"github.com/deepfabric/elasticell-operator/pkg/client/clientset/versioned"
	"github.com/deepfabric/elasticell-operator/pkg/controller"
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	notifyPort  = ":9998"
	proxyCfgStr = `
	{
		"addr": "0.0.0.0:6379",
		"addrNotify": ":9998",
		"watcherHeartbeatSec": 5,
		"pdAddrs": ["127.0.0.1:20801"],
		"maxRetries": 100,
		"retryDuration": 2000,
		"workerCount": 2,
		"supportCMDs": [
			"query",
			"ping",
			"set",
			"get",
			"mset",
			"mget",
			"incrby",
			"decrby",
			"getset",
			"append",
			"setnx",
			"strLen",
			"incr",
			"decr",
			"setrange",
			"msetnx",
			"hset",
			"hget",
			"hdel",
			"hexists",
			"hkeys",
			"hvals",
			"hgetall",
			"hscanget",
			"hlen",
			"hmget",
			"hmset",
			"hsetnx",
			"hstrlen",
			"hincrby",
			"lindex",
			"linsert",
			"llen",
			"lpop",
			"lpush",
			"lpushx",
			"lrange",
			"lrem",
			"lset",
			"ltrim",
			"rpop",
			"rpoplpush",
			"rpush",
			"rpushx",
			"sadd",
			"scard",
			"srem",
			"smembers",
			"sismember",
			"spop",
			"zadd",
			"zcard",
			"zcount",
			"zincrby",
			"zlexcount",
			"zrange",
			"zrangebylex",
			"zrangebyscore",
			"zrank",
			"zrem",
			"zremrangebylex",
			"zremrangebyrank",
			"zremrangebyscore",
			"zscore"
		]
	}
	`
)

// CellDiscovery helps new PD member to discover all other members in cluster bootstrap phase.
type CellDiscovery interface {
	Discover(string) (string, error)
	GetStoreConfig() (string, error)
	GetProxyConfig(string) (string, error)
}

type cellDiscovery struct {
	cli            versioned.Interface
	lock           sync.Mutex
	currentCluster *clusterInfo
	ccGetFn        func(ns, ccName string) (*v1alpha1.CellCluster, error)
	ccUpdateFn     func(ns string, cc *v1alpha1.CellCluster) (*v1alpha1.CellCluster, error)
	pdControl      controller.PDControlInterface
}

type clusterInfo struct {
	resourceVersion string
	peers           map[string]string
}

// NewCellDiscovery returns a CellDiscovery
func NewCellDiscovery(cli versioned.Interface) CellDiscovery {
	cd := &cellDiscovery{
		cli:            cli,
		pdControl:      controller.NewDefaultPDControl(),
		currentCluster: nil,
	}
	cd.ccGetFn = cd.realCCGetFn
	cd.ccUpdateFn = cd.realCCUpdateFn
	return cd
}

func (cd *cellDiscovery) Discover(advertisePeerURL string) (string, error) {
	cd.lock.Lock()
	defer cd.lock.Unlock()

	if advertisePeerURL == "" {
		return "", fmt.Errorf("advertisePeerURL is empty")
	}
	glog.Infof("advertisePeerURL is: %s", advertisePeerURL)
	strArr := strings.Split(advertisePeerURL, ".")
	if len(strArr) != 4 {
		return "", fmt.Errorf("advertisePeerURL format is wrong: %s", advertisePeerURL)
	}

	podName, pdServiceName, ns := strArr[0], strArr[1], strArr[2]
	ccName := strings.TrimSuffix(pdServiceName, "-pd-peer")
	podNamespace := os.Getenv("MY_POD_NAMESPACE")
	if ns != podNamespace {
		return "", fmt.Errorf("the peer's namespace: %s is not equal to discovery namespace: %s", ns, podNamespace)
	}
	cc, err := cd.ccGetFn(ns, ccName)
	if err != nil {
		return "", err
	}
	// TODO: the replicas should be the total replicas of pd sets.
	replicas := cc.Spec.PD.Replicas

	if cd.currentCluster == nil {
		cd.currentCluster = &clusterInfo{
			resourceVersion: cc.ResourceVersion,
			peers:           map[string]string{},
		}
	}
	cd.currentCluster.peers[podName] = advertisePeerURL
	initClusterParam := ""

	if cc.Spec.PdPeerURL != "" {
		glog.Infof("alreday get PdPeerURL: ", cc.Spec.PdPeerURL)
		return cc.Spec.PdPeerURL, nil
	}
	if len(cd.currentCluster.peers) == int(replicas) {
		pdPeerRPC := ""
		for podName, peerURL := range cd.currentCluster.peers {
			initClusterParam += fmt.Sprintf("%s=http://%s:2380,", podName, peerURL)
			pdPeerRPC += fmt.Sprintf("%s:20800,", peerURL)
		}
		initClusterParam = initClusterParam[:len(initClusterParam)-1]
		pdPeerRPC = pdPeerRPC[:len(pdPeerRPC)-1]
		cc.Spec.PdPeerURL = initClusterParam
		cc.Spec.PdPeerRPC = pdPeerRPC
		_, err = cd.ccUpdateFn(ns, cc)
		if err != nil {
			glog.Errorf("discovery svc failed to update cell cluster: ", err)
			return "", err
		}
		glog.Infof("discovery svc success to update cell cluster status")
		return initClusterParam, nil
	}
	return "", errors.New("please wait until all pd pod startup")

}

func (cd *cellDiscovery) realCCGetFn(ns, ccName string) (*v1alpha1.CellCluster, error) {
	return cd.cli.DeepfabricV1alpha1().CellClusters(ns).Get(ccName, metav1.GetOptions{})
}

func (cd *cellDiscovery) realCCUpdateFn(ns string, cc *v1alpha1.CellCluster) (*v1alpha1.CellCluster, error) {
	return cd.cli.DeepfabricV1alpha1().CellClusters(ns).Update(cc)
}

func (cd *cellDiscovery) GetStoreConfig() (string, error) {
	cd.lock.Lock()
	defer cd.lock.Unlock()

	ccName := os.Getenv("CLUSTER_NAME")
	ns := os.Getenv("MY_POD_NAMESPACE")

	cc, err := cd.ccGetFn(ns, ccName)
	if err != nil {
		return "", err
	}
	if cc.Spec.PdPeerRPC == "" {
		return "", errors.New("cell cluster status PdPeerRPC is null")
	}
	pdPeerRPC := cc.Spec.PdPeerRPC
	return pdPeerRPC, err

}

func (cd *cellDiscovery) GetProxyConfig(podIP string) (string, error) {
	cd.lock.Lock()
	defer cd.lock.Unlock()

	ccName := os.Getenv("CLUSTER_NAME")
	ns := os.Getenv("MY_POD_NAMESPACE")

	cc, err := cd.ccGetFn(ns, ccName)
	if err != nil {
		return "", err
	}
	if cc.Spec.PdPeerRPC == "" {
		return "", errors.New("cell cluster status PdPeerRPC is null")
	}
	pdPeerRPCs := strings.Split(cc.Spec.PdPeerRPC, ",")

	var proxyCfg interface{}
	fmt.Println("proxyCfgStr: ", proxyCfgStr)
	proxyConfig := []byte(proxyCfgStr)
	err = json.Unmarshal(proxyConfig, &proxyCfg)
	if err != nil {
		fmt.Println("decode proxyCfg error: ")
		fmt.Println(err)
	}

	proxyCfg.(map[string]interface{})["pdAddrs"] = pdPeerRPCs
	proxyCfg.(map[string]interface{})["addrNotify"] = podIP + notifyPort

	byteProxyCfg, err := json.Marshal(proxyCfg)
	return string(byteProxyCfg), err

}
