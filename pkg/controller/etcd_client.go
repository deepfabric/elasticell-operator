package controller

import (
	"context"
	"time"

	"github.com/coreos/etcd/clientv3"
)

const (
	timeOutThreshold = 2 * time.Second
)

func newCli(endpoint string) (*clientv3.Client, error) {
	endpoints := make([]string, 0)
	endpoints = append(endpoints, endpoint)
	cli, err := clientv3.New(
		clientv3.Config{
			Endpoints:   endpoints,
			DialTimeout: timeOutThreshold,
		},
	)
	return cli, err
}

func memberAdd(endpoint string, peerURL string) error {
	cli, err := newCli(endpoint)
	if err != nil {
		return err
	}
	defer cli.Close()

	peerURLs := make([]string, 0)
	peerURLs = append(peerURLs, peerURL)
	_, err = cli.MemberAdd(context.Background(), peerURLs)
	if err != nil {
	}
	return err
}

func memberRemoveById(endpoint string, id uint64) error {
	cli, err := newCli(endpoint)
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.MemberRemove(context.Background(), id)
	if err != nil {
		return err
	}
	return nil
}

func memberList(endpoint string) (*MembersInfo, error) {
	cli, err := newCli(endpoint)
	if err != nil {
		return nil, err
	}
	defer cli.Close()
	resp, err := cli.MemberList(context.Background())
	if err != nil {
		return nil, err
	}
	memberInfo := MembersInfo{}

	for _, member := range resp.Members {
		pdmember := new(PdMember)
		pdmember.MemberId = member.ID
		pdmember.Name = member.Name
		pdmember.PeerUrls = member.PeerURLs
		memberInfo.Members = append(memberInfo.Members, pdmember)
	}
	memberInfo.Leader = memberInfo.Members[0]
	return &memberInfo, nil
}

func getKV(endpoint string) (bool, error) {
	cli, err := newCli(endpoint)
	if err != nil {
		return false, err
	}
	defer cli.Close()
	_, err = cli.Get(context.Background(), "health")
	if err != nil {
		return false, err
	}
	return true, nil

}

func getHealth(endpoint string) (*HealthInfo, error) {
	cli, err := newCli(endpoint)
	if err != nil {
		return nil, err
	}
	defer cli.Close()
	resp, err := cli.MemberList(context.Background())
	if err != nil {
		return nil, err
	}

	membersHealth := make([]MemberHealth, 0)
	memberHealth := MemberHealth{}
	url := ""
	for _, member := range resp.Members {
		memberHealth.Name = member.Name
		memberHealth.MemberID = member.ID
		memberHealth.ClientUrls = member.ClientURLs
		if len(member.ClientURLs) > 0 {
			url = member.ClientURLs[0]
			health, _ := getKV(url)
			memberHealth.Health = health
		} else {
			memberHealth.Health = false
		}
		membersHealth = append(membersHealth, memberHealth)
	}
	return &HealthInfo{Healths: membersHealth}, nil
}
