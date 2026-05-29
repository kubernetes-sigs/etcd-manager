/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"testing"
	"time"

	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/etcdclient"
	"sigs.k8s.io/etcd-manager/pkg/privateapi"
)

func TestFindDiskReplacementCandidateMatchesUnhealthyMemberByName(t *testing.T) {
	clusterState := newReplacementTestClusterState()

	candidate := (&EtcdController{}).findDiskReplacementCandidate(clusterState)
	if candidate == nil {
		t.Fatalf("findDiskReplacementCandidate() returned nil")
	}
	if candidate.peerID != "etcd-a" {
		t.Fatalf("candidate peerID = %q, want etcd-a", candidate.peerID)
	}
	if candidate.memberID != "1" {
		t.Fatalf("candidate memberID = %q, want stale etcd member ID 1", candidate.memberID)
	}
}

func TestFindDiskReplacementCandidateNoOpsOnMultipleCandidates(t *testing.T) {
	clusterState := newReplacementTestClusterState()
	clusterState.members["4"] = &etcdclient.EtcdProcessMember{
		ID:         "4",
		Name:       "etcd-d",
		ClientURLs: []string{"https://127.0.0.4:4001"},
	}
	clusterState.peers["etcd-d"] = diskEmptyPeer("etcd-d")

	candidate := (&EtcdController{}).findDiskReplacementCandidate(clusterState)
	if candidate != nil {
		t.Fatalf("findDiskReplacementCandidate() = %v, want nil for ambiguous candidates", candidate)
	}
}

func TestFindDiskReplacementCandidateNoOpsWhenMatchingMemberHealthy(t *testing.T) {
	clusterState := newReplacementTestClusterState()
	clusterState.healthyMembers["1"] = clusterState.members["1"]

	candidate := (&EtcdController{}).findDiskReplacementCandidate(clusterState)
	if candidate != nil {
		t.Fatalf("findDiskReplacementCandidate() = %v, want nil when matching member is healthy", candidate)
	}
}

func TestFindDiskReplacementCandidateNoOpsWhenDiskEmptyPeerHasClusterState(t *testing.T) {
	clusterState := newReplacementTestClusterState()
	clusterState.peers["etcd-a"].info.EtcdState = &protoetcd.EtcdState{
		Cluster: &protoetcd.EtcdCluster{ClusterToken: "token"},
	}

	candidate := (&EtcdController{}).findDiskReplacementCandidate(clusterState)
	if candidate != nil {
		t.Fatalf("findDiskReplacementCandidate() = %v, want nil when peer has cluster state", candidate)
	}
}

func TestDiskReplacementCandidateDeadlineUsesStaleMemberID(t *testing.T) {
	now := time.Now()
	candidate := &diskReplacementCandidate{
		peerID:   "etcd-a",
		memberID: "12345",
		member: &etcdclient.EtcdProcessMember{
			ID:   "12345",
			Name: "etcd-a",
		},
	}

	controller := &EtcdController{
		peerState: map[privateapi.PeerId]*peerState{
			"etcd-a": {
				lastEtcdHealthy: now.Add(-2 * removeUnhealthyDeadline),
			},
		},
	}
	if controller.diskReplacementCandidatePastDeadline(candidate, now) {
		t.Fatalf("diskReplacementCandidatePastDeadline() used peer name health history, want stale member ID history")
	}

	controller.peerState[privateapi.PeerId(candidate.memberID)] = &peerState{
		lastEtcdHealthy: now.Add(-2 * removeUnhealthyDeadline),
	}
	if !controller.diskReplacementCandidatePastDeadline(candidate, now) {
		t.Fatalf("diskReplacementCandidatePastDeadline() = false, want true using stale member ID history")
	}
}

func newReplacementTestClusterState() *etcdClusterState {
	members := map[EtcdMemberId]*etcdclient.EtcdProcessMember{
		"1": {
			ID:         "1",
			Name:       "etcd-a",
			ClientURLs: []string{"https://127.0.0.1:4001"},
		},
		"2": {
			ID:         "2",
			Name:       "etcd-b",
			ClientURLs: []string{"https://127.0.0.2:4001"},
		},
		"3": {
			ID:         "3",
			Name:       "etcd-c",
			ClientURLs: []string{"https://127.0.0.3:4001"},
		},
	}

	return &etcdClusterState{
		members: members,
		healthyMembers: map[EtcdMemberId]*etcdclient.EtcdProcessMember{
			"2": members["2"],
			"3": members["3"],
		},
		peers: map[privateapi.PeerId]*etcdClusterPeerInfo{
			"etcd-a": diskEmptyPeer("etcd-a"),
			"etcd-b": configuredPeer("etcd-b"),
			"etcd-c": configuredPeer("etcd-c"),
		},
	}
}

func diskEmptyPeer(name string) *etcdClusterPeerInfo {
	return &etcdClusterPeerInfo{
		peer: &peer{Id: privateapi.PeerId(name)},
		info: &protoetcd.GetInfoResponse{
			DiskEmpty: true,
			NodeConfiguration: &protoetcd.EtcdNode{
				Name: name,
			},
		},
	}
}

func configuredPeer(name string) *etcdClusterPeerInfo {
	return &etcdClusterPeerInfo{
		peer: &peer{Id: privateapi.PeerId(name)},
		info: &protoetcd.GetInfoResponse{
			NodeConfiguration: &protoetcd.EtcdNode{
				Name: name,
			},
			EtcdState: &protoetcd.EtcdState{
				Cluster: &protoetcd.EtcdCluster{ClusterToken: "token"},
			},
		},
	}
}
