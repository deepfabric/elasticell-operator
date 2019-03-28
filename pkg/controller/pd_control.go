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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/deepfabric/elasticell-operator/pkg/apis/deepfabric.com/v1alpha1"
	pdapi "github.com/deepfabric/elasticell/pkg/pdapi"
)

const (
	timeout = 5 * time.Second
	// to do check time.Second
	MaxStoreDownTimeDuration = 3600 * 1000 * 1000 * 1000
	// to do which state is tombstone
	TombStoneStoreState = 1
)

// PDControlInterface is an interface that knows how to manage and get cell cluster's PD client
type PDControlInterface interface {
	// GetPDClient provides PDClient of the cell cluster.
	GetPDClient(cc *v1alpha1.CellCluster) PDClient
}

// defaultPDControl is the default implementation of PDControlInterface.
type defaultPDControl struct {
	mutex     sync.Mutex
	pdClients map[string]PDClient
}

// NewDefaultPDControl returns a defaultPDControl instance
func NewDefaultPDControl() PDControlInterface {
	return &defaultPDControl{pdClients: map[string]PDClient{}}
}

// GetPDClient provides a PDClient of real pd cluster,if the PDClient not existing, it will create new one.
func (pdc *defaultPDControl) GetPDClient(cc *v1alpha1.CellCluster) PDClient {
	pdc.mutex.Lock()
	defer pdc.mutex.Unlock()
	namespace := cc.GetNamespace()
	ccName := cc.GetName()
	key := pdClientKey(namespace, ccName)
	if _, ok := pdc.pdClients[key]; !ok {
		pdc.pdClients[key] = NewPDClient(pdClientURL(namespace, ccName), timeout)
	}
	return pdc.pdClients[key]
}

// pdClientKey returns the pd client key
func pdClientKey(namespace, clusterName string) string {
	return fmt.Sprintf("%s.%s", clusterName, namespace)
}

// pdClientUrl builds the url of pd client
func pdClientURL(namespace, clusterName string) string {
	return fmt.Sprintf("http://%s-pd.%s:2379", clusterName, namespace)
}

// PDClient provides pd server's api
type PDClient interface {
	// GetHealth returns the PD's health info
	GetHealth() (*HealthInfo, error)
	// GetConfig returns PD's config
	GetConfig() (time.Duration, error)
	// GetCluster returns used when syncing pod labels.
	GetCluster() (string, error)
	// GetMembers returns all PD members from cluster
	GetMembers() (*MembersInfo, error)
	// AddMember add new member to etcd cluster
	// AddMember(peerURL string) error
	// DeleteMember deletes a PD member from cluster
	DeleteMember(name string) error
	// DeleteMemberByID deletes a PD member from cluster
	DeleteMemberByID(memberID uint64) error
	// GetStores lists all TiKV stores from cluster
	GetStores() (*StoresInfo, error)
	// GetTombStoneStores lists all tombstone stores from cluster
	GetTombStoneStores() (*StoresInfo, error)
	// GetStore gets a TiKV store for a specific store id from cluster
	GetStore(storeID uint64) (*pdapi.StoreInfo, error)
	// storeLabelsEqualNodeLabels compares store labels with node labels
	// for historic reasons, PD stores TiKV labels as []*StoreLabel which is a key-value pair slice
	SetStoreLabels(storeID uint64, labels map[string]string) (bool, error)
	// DeleteStore deletes a TiKV store from cluster
	DeleteStore(storeID uint64) error

	// BeginEvictLeader initiates leader eviction for a storeID.
	// This is used when upgrading a pod.
	BeginEvictLeader(storeID uint64) error
	// EndEvictLeader is used at the end of pod upgrade.
	EndEvictLeader(storeID uint64) error
	// GetEvictLeaderSchedulers gets schedulers of evict leader
	GetEvictLeaderSchedulers() ([]string, error)
	// GetPDLeader returns pd leader
	GetPDLeader() (*PdMember, error)
	// TransferPDLeader transfers pd leader to specified member
	TransferPDLeader(name string) error
}

var (
	healthPrefix  = "pd/health"
	membersPrefix = "pd/api/v1/members"
	storesPrefix  = "pd/api/v1/stores"
	// configPrefix           = "pd/api/v1/config"
	// clusterIDPrefix        = "pd/api/v1/cluster"
	// schedulersPrefix       = "pd/api/v1/schedulers"
	// pdLeaderPrefix         = "pd/api/v1/leader"
	// pdLeaderTransferPrefix = "pd/api/v1/leader/transfer"
)

// pdClient is default implementation of PDClient
type pdClient struct {
	url        string
	endpoint   string
	httpClient *http.Client
}

// NewPDClient returns a new PDClient
func NewPDClient(url string, timeout time.Duration) PDClient {
	return &pdClient{
		url:        url,
		endpoint:   strings.TrimPrefix(url, "http://"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

// HealthInfo define PD's healthy info
type HealthInfo struct {
	Healths []MemberHealth
}

// MemberHealth define a pd member's healthy info
type MemberHealth struct {
	Name       string
	MemberID   uint64
	ClientUrls []string
	Health     bool
}

// MembersInfo is PD members info returned from PD RESTful interface
//type Members map[string][]*PdMember
type PdMember struct {
	MemberId uint64
	Name     string
	PeerUrls []string
}
type MembersInfo struct {
	Members []*PdMember
	Leader  *PdMember
}

// StoresInfo is stores info returned from PD RESTful interface
type StoresInfo struct {
	Count  int
	Code   int                `json:"code"`
	Err    string             `json:"error"`
	Stores []*pdapi.StoreInfo `json:"value"`
}

// StoreInfo is stores info returned from PD RESTful interface
type StoreInfo struct {
	Code  int              `json:"code"`
	Err   string           `json:"error"`
	Store *pdapi.StoreInfo `json:"value"`
}

type schedulerInfo struct {
	Name    string `json:"name"`
	StoreID uint64 `json:"store_id"`
}

func (pc *pdClient) GetHealth() (*HealthInfo, error) {
	return getHealth(pc.endpoint)
}

func (pc *pdClient) GetConfig() (time.Duration, error) {
	// need new api
	return MaxStoreDownTimeDuration, nil
}

func (pc *pdClient) GetCluster() (string, error) {
	// need new api
	return "elasticell-cluster-demo", nil
}

/*
func (pc *pdClient) AddMember(peerURL string) error {
	return memberAdd(pc.endpoint,peerURL)
}
*/

func (pc *pdClient) GetMembers() (*MembersInfo, error) {
	members, err := memberList(pc.endpoint)
	if err != nil {
		return nil, err
	}
	return members, nil
}

func (pc *pdClient) DeleteMemberByID(memberID uint64) error {
	var exist bool
	members, err := memberList(pc.endpoint)
	if err != nil {
		return err
	}
	for _, member := range members.Members {
		if member.MemberId == memberID {
			exist = true
			break
		}
	}
	if !exist {
		return nil
	}

	return memberRemoveById(pc.endpoint, memberID)
}

func (pc *pdClient) DeleteMember(name string) error {
	var exist bool
	var memberID uint64
	members, err := pc.GetMembers()
	if err != nil {
		return err
	}
	for _, member := range members.Members {
		if member.Name == name {
			exist = true
			memberID = member.MemberId
			break
		}
	}
	if !exist {
		return nil
	}
	return memberRemoveById(pc.endpoint, memberID)
}

func (pc *pdClient) GetStores() (*StoresInfo, error) {
	apiURL := fmt.Sprintf("%s/%s", pc.url, storesPrefix)
	body, err := pc.getBodyOK(apiURL)
	if err != nil {
		return nil, err
	}
	storesInfo := &StoresInfo{}
	err = json.Unmarshal(body, storesInfo)
	if err != nil {
		return nil, err
	}
	if storesInfo.Code != 0 {
		return nil, errors.New(storesInfo.Err)
	}
	return storesInfo, nil
}

func (pc *pdClient) GetTombStoneStores() (*StoresInfo, error) {
	apiURL := fmt.Sprintf("%s/%s", pc.url, storesPrefix)
	body, err := pc.getBodyOK(apiURL)
	if err != nil {
		return nil, err
	}
	storesInfo := &StoresInfo{}
	err = json.Unmarshal(body, storesInfo)
	if err != nil {
		return nil, err
	}
	if storesInfo.Code != 0 {
		return nil, errors.New(storesInfo.Err)
	}

	tomStoneStores := &StoresInfo{}

	for _, store := range storesInfo.Stores {
		if store.Meta.State == TombStoneStoreState {
			tomStoneStores.Stores = append(tomStoneStores.Stores, store)
		}
	}
	return tomStoneStores, nil
}

func (pc *pdClient) GetStore(storeID uint64) (*pdapi.StoreInfo, error) {
	apiURL := fmt.Sprintf("%s/%s/%d", pc.url, storesPrefix, storeID)
	body, err := pc.getBodyOK(apiURL)
	if err != nil {
		return nil, err
	}
	storeInfo := &StoreInfo{}
	err = json.Unmarshal(body, storeInfo)
	if err != nil {
		return nil, err
	}
	fmt.Println("storeInfo: ", storeInfo)
	if storeInfo.Code != 0 {
		return nil, errors.New(storeInfo.Err)
	}
	return storeInfo.Store, nil
}

func (pc *pdClient) DeleteStore(storeID uint64) error {

	_, err := pc.GetStore(storeID)
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("%s/%s/%d", pc.url, storesPrefix, storeID)
	req, err := http.NewRequest("DELETE", apiURL, nil)
	if err != nil {
		return err
	}
	res, err := pc.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer DeferClose(res.Body, &err)

	// Remove an offline store should returns http.StatusOK
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound {
		return nil
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}

	return fmt.Errorf("failed to delete store %d: %v", storeID, string(body))

}

func (pc *pdClient) SetStoreLabels(storeID uint64, labels map[string]string) (bool, error) {
	// to do
	return true, nil
}

func (pc *pdClient) BeginEvictLeader(storeID uint64) error {
	// to do
	return nil
}

func (pc *pdClient) EndEvictLeader(storeID uint64) error {
	// to do
	return nil
}

func (pc *pdClient) GetEvictLeaderSchedulers() ([]string, error) {
	// to do
	return nil, nil
}

func (pc *pdClient) GetPDLeader() (*PdMember, error) {
	// to do
	return nil, nil
}

func (pc *pdClient) TransferPDLeader(memberName string) error {
	// to do
	return nil
}

func (pc *pdClient) getBodyOK(apiURL string) ([]byte, error) {
	res, err := pc.httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer DeferClose(res.Body, &err)
	if res.StatusCode >= 400 {
		errMsg := fmt.Errorf(fmt.Sprintf("Error response %v", res.StatusCode))
		return nil, errMsg
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return body, err
}

// DeferClose captures the error returned from closing (if an error occurs).
// This is designed to be used in a defer statement.
func DeferClose(c io.Closer, err *error) {
	if cerr := c.Close(); cerr != nil && *err == nil {
		*err = cerr
	}
}

type FakePDControl struct {
	defaultPDControl
}

func NewFakePDControl() *FakePDControl {
	return &FakePDControl{
		defaultPDControl{pdClients: map[string]PDClient{}},
	}
}

func (fpc *FakePDControl) SetPDClient(cc *v1alpha1.CellCluster, pdclient PDClient) {
	fpc.defaultPDControl.pdClients[pdClientKey(cc.Namespace, cc.Name)] = pdclient
}

type ActionType string

const (
	GetHealthActionType                ActionType = "GetHealth"
	GetConfigActionType                ActionType = "GetConfig"
	GetClusterActionType               ActionType = "GetCluster"
	GetMembersActionType               ActionType = "GetMembers"
	GetStoresActionType                ActionType = "GetStores"
	GetTombStoneStoresActionType       ActionType = "GetTombStoneStores"
	GetStoreActionType                 ActionType = "GetStore"
	DeleteStoreActionType              ActionType = "DeleteStore"
	DeleteMemberByIDActionType         ActionType = "DeleteMemberByID"
	DeleteMemberActionType             ActionType = "DeleteMember "
	SetStoreLabelsActionType           ActionType = "SetStoreLabels"
	BeginEvictLeaderActionType         ActionType = "BeginEvictLeader"
	EndEvictLeaderActionType           ActionType = "EndEvictLeader"
	GetEvictLeaderSchedulersActionType ActionType = "GetEvictLeaderSchedulers"
	GetPDLeaderActionType              ActionType = "GetPDLeader"
	TransferPDLeaderActionType         ActionType = "TransferPDLeader"
)

type NotFoundReaction struct {
	actionType ActionType
}

func (nfr *NotFoundReaction) Error() string {
	return fmt.Sprintf("not found %s reaction. Please add the reaction", nfr.actionType)
}

type Action struct {
	ID     uint64
	Name   string
	Labels map[string]string
}

type Reaction func(action *Action) (interface{}, error)

type FakePDClient struct {
	reactions map[ActionType]Reaction
}

func NewFakePDClient() *FakePDClient {
	return &FakePDClient{reactions: map[ActionType]Reaction{}}
}

func (pc *FakePDClient) AddReaction(actionType ActionType, reaction Reaction) {
	pc.reactions[actionType] = reaction
}

// fakeAPI is a small helper for fake API calls
func (pc *FakePDClient) fakeAPI(actionType ActionType, action *Action) (interface{}, error) {
	if reaction, ok := pc.reactions[actionType]; ok {
		result, err := reaction(action)
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	return nil, &NotFoundReaction{actionType}
}

func (pc *FakePDClient) GetHealth() (*HealthInfo, error) {
	action := &Action{}
	result, err := pc.fakeAPI(GetHealthActionType, action)
	if err != nil {
		return nil, err
	}
	return result.(*HealthInfo), nil
}

func (pc *FakePDClient) GetConfig() (time.Duration, error) {
	action := &Action{}
	_, err := pc.fakeAPI(GetConfigActionType, action)
	if err != nil {
		return 0, err
	}
	return MaxStoreDownTimeDuration, nil
}

func (pc *FakePDClient) GetCluster() (string, error) {
	action := &Action{}
	_, err := pc.fakeAPI(GetClusterActionType, action)
	if err != nil {
		return "", err
	}
	return "elasticell-cluster-demo", nil
}

func (pc *FakePDClient) GetMembers() (*MembersInfo, error) {
	action := &Action{}
	result, err := pc.fakeAPI(GetMembersActionType, action)
	if err != nil {
		return nil, err
	}
	return result.(*MembersInfo), nil
}

func (pc *FakePDClient) GetStores() (*StoresInfo, error) {
	action := &Action{}
	result, err := pc.fakeAPI(GetStoresActionType, action)
	if err != nil {
		return nil, err
	}
	return result.(*StoresInfo), nil
}

func (pc *FakePDClient) GetTombStoneStores() (*StoresInfo, error) {
	action := &Action{}
	result, err := pc.fakeAPI(GetTombStoneStoresActionType, action)
	if err != nil {
		return nil, err
	}
	return result.(*StoresInfo), nil
}

func (pc *FakePDClient) GetStore(id uint64) (*pdapi.StoreInfo, error) {
	action := &Action{
		ID: id,
	}
	result, err := pc.fakeAPI(GetStoreActionType, action)
	if err != nil {
		return nil, err
	}
	return result.(*pdapi.StoreInfo), nil
}

func (pc *FakePDClient) DeleteStore(id uint64) error {
	if reaction, ok := pc.reactions[DeleteStoreActionType]; ok {
		action := &Action{ID: id}
		_, err := reaction(action)
		return err
	}
	return nil
}

func (pc *FakePDClient) DeleteMemberByID(id uint64) error {
	if reaction, ok := pc.reactions[DeleteMemberByIDActionType]; ok {
		action := &Action{ID: id}
		_, err := reaction(action)
		return err
	}
	return nil
}

func (pc *FakePDClient) DeleteMember(name string) error {
	if reaction, ok := pc.reactions[DeleteMemberActionType]; ok {
		action := &Action{Name: name}
		_, err := reaction(action)
		return err
	}
	return nil
}

// SetStoreLabels sets TiKV labels
func (pc *FakePDClient) SetStoreLabels(storeID uint64, labels map[string]string) (bool, error) {
	if reaction, ok := pc.reactions[SetStoreLabelsActionType]; ok {
		action := &Action{ID: storeID, Labels: labels}
		result, err := reaction(action)
		return result.(bool), err
	}
	return true, nil
}

func (pc *FakePDClient) BeginEvictLeader(storeID uint64) error {
	if reaction, ok := pc.reactions[BeginEvictLeaderActionType]; ok {
		action := &Action{ID: storeID}
		_, err := reaction(action)
		return err
	}
	return nil
}

func (pc *FakePDClient) EndEvictLeader(storeID uint64) error {
	if reaction, ok := pc.reactions[EndEvictLeaderActionType]; ok {
		action := &Action{ID: storeID}
		_, err := reaction(action)
		return err
	}
	return nil
}

func (pc *FakePDClient) GetEvictLeaderSchedulers() ([]string, error) {
	if reaction, ok := pc.reactions[GetEvictLeaderSchedulersActionType]; ok {
		action := &Action{}
		result, err := reaction(action)
		return result.([]string), err
	}
	return nil, nil
}

func (pc *FakePDClient) GetPDLeader() (*PdMember, error) {
	if reaction, ok := pc.reactions[GetPDLeaderActionType]; ok {
		action := &Action{}
		result, err := reaction(action)
		return result.(*PdMember), err
	}
	return nil, nil
}

func (pc *FakePDClient) TransferPDLeader(memberName string) error {
	if reaction, ok := pc.reactions[TransferPDLeaderActionType]; ok {
		action := &Action{Name: memberName}
		_, err := reaction(action)
		return err
	}
	return nil
}
