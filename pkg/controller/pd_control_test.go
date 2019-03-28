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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deepfabric/elasticell/pkg/pb/metapb"

	"github.com/deepfabric/elasticell/pkg/pdapi"
	. "github.com/onsi/gomega"
)

const (
	ContentTypeJSON string = "application/json"
)

var (
	id1 uint64 = 708162861580590150
	id2 uint64 = 11422760369413494203
	id3 uint64 = 7956610951254538789
)

func getClientServer(h func(http.ResponseWriter, *http.Request)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(h))
	return srv
}

func TestHealth(t *testing.T) {
	g := NewGomegaWithT(t)
	healths := []MemberHealth{
		{Name: "pd1", MemberID: id1, ClientUrls: []string{"http://0.0.0.0:2371"}, Health: true},
		{Name: "pd3", MemberID: id3, ClientUrls: []string{"http://0.0.0.0:2373"}, Health: true},
		{Name: "pd2", MemberID: id2, ClientUrls: []string{"http://0.0.0.0:2372"}, Health: true},
	}
	ccs := []struct {
		caseName string
		endpoint string
		want     []MemberHealth
	}{{
		caseName: "GetHealth",
		endpoint: "127.0.0.1:2371",
		want:     healths,
	}}

	for _, cc := range ccs {
		pdClient := NewPDClient(cc.endpoint, timeout)
		result, err := pdClient.GetHealth()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(Equal(&HealthInfo{healths}))
	}
}

func TestGetMembers(t *testing.T) {
	g := NewGomegaWithT(t)
	member1 := &PdMember{Name: "pd1", MemberId: id1, PeerUrls: []string{"http://127.0.0.1:2381"}}
	member2 := &PdMember{Name: "pd2", MemberId: id2, PeerUrls: []string{"http://127.0.0.1:2382"}}
	member3 := &PdMember{Name: "pd3", MemberId: id3, PeerUrls: []string{"http://127.0.0.1:2383"}}

	members := &MembersInfo{
		Members: []*PdMember{
			member1,
			member3,
			member2,
		},
		Leader: member1,
	}

	ccs := []struct {
		caseName string
		endpoint string
		want     *MembersInfo
	}{
		{
			caseName: "GetMembers",
			endpoint: "127.0.0.1:2371",
			want:     members,
		},
	}

	for _, cc := range ccs {

		pdClient := NewPDClient(cc.endpoint, timeout)
		result, err := pdClient.GetMembers()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(Equal(members))
	}
}

func TestGetStores(t *testing.T) {
	g := NewGomegaWithT(t)
	store1 := &pdapi.StoreInfo{
		Meta: metapb.Store{ID: 1, State: metapb.UP},
	}
	store2 := &pdapi.StoreInfo{
		Meta: metapb.Store{ID: 2, State: metapb.UP},
	}
	stores := &StoresInfo{
		Count: 2,
		Stores: []*pdapi.StoreInfo{
			store1,
			store2,
		},
	}

	storesBytes, err := json.Marshal(stores)
	g.Expect(err).NotTo(HaveOccurred())

	ccs := []struct {
		caseName string
		path     string
		method   string
		resp     []byte
		want     *StoresInfo
	}{{
		caseName: "GetStores",
		path:     fmt.Sprintf("/%s", storesPrefix),
		method:   "GET",
		resp:     storesBytes,
		want:     stores,
	}}

	for _, cc := range ccs {
		svc := getClientServer(func(w http.ResponseWriter, request *http.Request) {
			g.Expect(request.Method).To(Equal(cc.method), "check method")
			g.Expect(request.URL.Path).To(Equal(cc.path), "check url")

			w.Header().Set("Content-Type", ContentTypeJSON)
			w.Write(cc.resp)
		})
		defer svc.Close()
		fmt.Println("svc.URL: ", svc.URL)
		pdClient := NewPDClient(svc.URL, timeout)
		result, err := pdClient.GetStores()
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(Equal(stores))
	}
}

func TestGetStore(t *testing.T) {
	g := NewGomegaWithT(t)

	id := uint64(1)
	store := &pdapi.StoreInfo{
		Meta: metapb.Store{ID: id, State: metapb.UP},
	}
	storeinfo := &StoreInfo{
		Code:  0,
		Err:   "",
		Store: store,
	}

	storeBytes, err := json.Marshal(storeinfo)
	g.Expect(err).NotTo(HaveOccurred())

	ccs := []struct {
		caseName string
		path     string
		method   string
		id       uint64
		resp     []byte
		want     *pdapi.StoreInfo
	}{{
		caseName: "GetStore",
		path:     fmt.Sprintf("/%s/%d", storesPrefix, id),
		method:   "GET",
		id:       id,
		resp:     storeBytes,
		want:     store,
	}}

	for _, cc := range ccs {
		svc := getClientServer(func(w http.ResponseWriter, request *http.Request) {
			g.Expect(request.Method).To(Equal(cc.method), "test method")
			g.Expect(request.URL.Path).To(Equal(cc.path), "test url")

			w.Header().Set("Content-Type", ContentTypeJSON)
			w.Write(cc.resp)
		})
		defer svc.Close()

		pdClient := NewPDClient(svc.URL, timeout)
		result, err := pdClient.GetStore(cc.id)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(Equal(store))
	}
}

/*
func TestSetStoreLabels(t *testing.T) {
	g := NewGomegaWithT(t)
	id := uint64(1)
	labels := map[string]string{"testkey": "testvalue"}
	ccs := []struct {
		caseName string
		path     string
		method   string
		want     bool
	}{{
		caseName: "success_SetStoreLabels",
		path:     fmt.Sprintf("/%s/%d/label", storesPrefix, id),
		method:   "POST",
		want:     true,
	}, {
		caseName: "failed_SetStoreLabels",
		path:     fmt.Sprintf("/%s/%d/label", storesPrefix, id),
		method:   "POST",
		want:     false,
	},
	}

	for _, cc := range ccs {
		svc := getClientServer(func(w http.ResponseWriter, request *http.Request) {
			g.Expect(request.Method).To(Equal(cc.method), "check method")
			g.Expect(request.URL.Path).To(Equal(cc.path), "check url")

			labels := &map[string]string{}
			err := readJSON(request.Body, labels)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(labels).To(Equal(labels), "check labels")

			w.Header().Set("Content-Type", ContentTypeJSON)
			if cc.want {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		})
		defer svc.Close()

		pdClient := NewPDClient(svc.URL, timeout)
		result, _ := pdClient.SetStoreLabels(id, labels)
		g.Expect(result).To(Equal(cc.want))
	}
}

func TestDeleteMember(t *testing.T) {
	g := NewGomegaWithT(t)
	name := "testMember"
	member := &PdMember{Name: name, MemberId: uint64(1)}
	membersExist := &MembersInfo{
		Members: []*PdMember{
			member,
		},
		Leader: member,
	}
	membersExistBytes, err := json.Marshal(membersExist)
	g.Expect(err).NotTo(HaveOccurred())

	membersNotExist := &MembersInfo{
		Members: []*PdMember{},
	}
	membersNotExistBytes, err := json.Marshal(membersNotExist)
	g.Expect(err).NotTo(HaveOccurred())

	ccs := []struct {
		caseName  string
		prePath   string
		preMethod string
		preResp   []byte
		exist     bool
		path      string
		method    string
		want      bool
	}{{
		caseName:  "success_DeleteMember",
		prePath:   fmt.Sprintf("/%s", membersPrefix),
		preMethod: "GET",
		preResp:   membersExistBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/name/%s", membersPrefix, name),
		method:    "DELETE",
		want:      true,
	}, {
		caseName:  "failed_DeleteMember",
		prePath:   fmt.Sprintf("/%s", membersPrefix),
		preMethod: "GET",
		preResp:   membersExistBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/name/%s", membersPrefix, name),
		method:    "DELETE",
		want:      false,
	}, {
		caseName:  "delete_not_exist_member",
		prePath:   fmt.Sprintf("/%s", membersPrefix),
		preMethod: "GET",
		preResp:   membersNotExistBytes,
		exist:     false,
		path:      fmt.Sprintf("/%s/name/%s", membersPrefix, name),
		method:    "DELETE",
		want:      true,
	},
	}

	for _, cc := range ccs {
		count := 1
		svc := getClientServer(func(w http.ResponseWriter, request *http.Request) {
			if count == 1 {
				g.Expect(request.Method).To(Equal(cc.preMethod), "check method")
				g.Expect(request.URL.Path).To(Equal(cc.prePath), "check url")
				w.Header().Set("Content-Type", ContentTypeJSON)
				w.WriteHeader(http.StatusOK)
				w.Write(cc.preResp)
				count++
				return
			}

			g.Expect(cc.exist).To(BeTrue())
			g.Expect(request.Method).To(Equal(cc.method), "check method")
			g.Expect(request.URL.Path).To(Equal(cc.path), "check url")
			w.Header().Set("Content-Type", ContentTypeJSON)
			if cc.want {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		})
		defer svc.Close()

		pdClient := NewPDClient(svc.URL, timeout)
		err := pdClient.DeleteMember(name)
		if cc.want {
			g.Expect(err).NotTo(HaveOccurred(), "check result")
		} else {
			g.Expect(err).To(HaveOccurred(), "check result")
		}
	}
}

func TestDeleteMemberByID(t *testing.T) {
	g := NewGomegaWithT(t)
	id := uint64(1)
	member := &PdMember{Name: "test", MemberId: id}
	membersExist := &MembersInfo{
		Members: []*PdMember{
			member,
		},
		Leader: member,
	}
	membersExistBytes, err := json.Marshal(membersExist)
	g.Expect(err).NotTo(HaveOccurred())

	membersNotExist := &MembersInfo{
		Members: []*PdMember{},
	}
	membersNotExistBytes, err := json.Marshal(membersNotExist)
	g.Expect(err).NotTo(HaveOccurred())

	ccs := []struct {
		caseName  string
		prePath   string
		preMethod string
		preResp   []byte
		exist     bool
		path      string
		method    string
		want      bool
	}{{
		caseName:  "success_DeleteMemberByID",
		prePath:   fmt.Sprintf("/%s", membersPrefix),
		preMethod: "GET",
		preResp:   membersExistBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/id/%d", membersPrefix, id),
		method:    "DELETE",
		want:      true,
	}, {
		caseName:  "failed_DeleteMemberByID",
		prePath:   fmt.Sprintf("/%s", membersPrefix),
		preMethod: "GET",
		preResp:   membersExistBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/id/%d", membersPrefix, id),
		method:    "DELETE",
		want:      false,
	}, {
		caseName:  "delete_not_exit_member",
		prePath:   fmt.Sprintf("/%s", membersPrefix),
		preMethod: "GET",
		preResp:   membersNotExistBytes,
		exist:     false,
		path:      fmt.Sprintf("/%s/id/%d", membersPrefix, id),
		method:    "DELETE",
		want:      true,
	},
	}

	for _, cc := range ccs {
		count := 1
		svc := getClientServer(func(w http.ResponseWriter, request *http.Request) {
			if count == 1 {
				g.Expect(request.Method).To(Equal(cc.preMethod), "check method")
				g.Expect(request.URL.Path).To(Equal(cc.prePath), "check url")
				w.Header().Set("Content-Type", ContentTypeJSON)
				w.WriteHeader(http.StatusOK)
				w.Write(cc.preResp)
				count++
				return
			}

			g.Expect(cc.exist).To(BeTrue())
			g.Expect(request.Method).To(Equal(cc.method), "check method")
			g.Expect(request.URL.Path).To(Equal(cc.path), "check url")
			w.Header().Set("Content-Type", ContentTypeJSON)
			if cc.want {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		})
		defer svc.Close()

		pdClient := NewPDClient(svc.URL, timeout)
		err := pdClient.DeleteMemberByID(id)
		if cc.want {
			g.Expect(err).NotTo(HaveOccurred(), "check result")
		} else {
			g.Expect(err).To(HaveOccurred(), "check result")
		}
	}
}

func readJSON(r io.ReadCloser, data interface{}) error {
	defer r.Close()

	b, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	err = json.Unmarshal(b, data)
	if err != nil {
		return err
	}

	return nil
}

*/
func TestDeleteStore(t *testing.T) {
	g := NewGomegaWithT(t)
	storeID := uint64(1)
	store := &pdapi.StoreInfo{
		Meta: metapb.Store{ID: storeID, State: metapb.UP},
	}
	storeinfo := &StoreInfo{
		Code:  0,
		Err:   "",
		Store: store,
	}

	storesBytes, err := json.Marshal(storeinfo)
	g.Expect(err).NotTo(HaveOccurred())

	ccs := []struct {
		caseName  string
		prePath   string
		preMethod string
		preResp   []byte
		exist     bool
		path      string
		method    string
		want      bool
	}{{
		caseName:  "success_DeleteStore",
		prePath:   fmt.Sprintf("/%s/%d", storesPrefix, storeID),
		preMethod: "GET",
		preResp:   storesBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/%d", storesPrefix, storeID),
		method:    "DELETE",
		want:      true,
	}, {
		caseName:  "failed_DeleteStore",
		prePath:   fmt.Sprintf("/%s/%d", storesPrefix, storeID),
		preMethod: "GET",
		preResp:   storesBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/%d", storesPrefix, storeID),
		method:    "DELETE",
		want:      false,
	}, {
		caseName:  "delete_not_exist_store",
		prePath:   fmt.Sprintf("/%s/%d", storesPrefix, storeID),
		preMethod: "GET",
		preResp:   storesBytes,
		exist:     true,
		path:      fmt.Sprintf("/%s/%d", storesPrefix, storeID),
		method:    "DELETE",
		want:      true,
	},
	}

	for _, cc := range ccs {
		count := 1
		svc := getClientServer(func(w http.ResponseWriter, request *http.Request) {
			if count == 1 {
				g.Expect(request.Method).To(Equal(cc.preMethod), "check method")
				g.Expect(request.URL.Path).To(Equal(cc.prePath), "check url")
				w.Header().Set("Content-Type", ContentTypeJSON)
				w.WriteHeader(http.StatusOK)
				w.Write(cc.preResp)
				count++
				return
			}

			g.Expect(cc.exist).To(BeTrue())
			g.Expect(request.Method).To(Equal(cc.method), "check method")
			g.Expect(request.URL.Path).To(Equal(cc.path), "check url")

			w.Header().Set("Content-Type", ContentTypeJSON)
			if cc.want {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusInternalServerError)
			}
		})
		defer svc.Close()
		fmt.Println(cc.caseName)
		pdClient := NewPDClient(svc.URL, timeout)
		err := pdClient.DeleteStore(storeID)
		if cc.want {
			g.Expect(err).NotTo(HaveOccurred(), "check result")
		} else {
			g.Expect(err).To(HaveOccurred(), "check result")
		}
	}
}
