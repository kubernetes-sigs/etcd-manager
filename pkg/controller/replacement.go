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
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/klog/v2"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/etcdclient"
	"sigs.k8s.io/etcd-manager/pkg/privateapi"
)

type etcdMemberRef struct {
	id     EtcdMemberId
	member *etcdclient.EtcdProcessMember
}

type diskReplacementCandidate struct {
	peerID   privateapi.PeerId
	peer     *etcdClusterPeerInfo
	memberID EtcdMemberId
	member   *etcdclient.EtcdProcessMember
}

func (c diskReplacementCandidate) String() string {
	return fmt.Sprintf("peer=%q memberID=%q memberName=%q", c.peerID, c.memberID, c.member.Name)
}

func (m *EtcdController) maybeRemoveStaleMemberForEmptyDiskReplacement(ctx context.Context, clusterSpec *protoetcd.ClusterSpec, clusterState *etcdClusterState, versionMismatch []*etcdClusterPeerInfo, quarantinedMembers int) (bool, error) {
	desiredMemberCount := int(clusterSpec.MemberCount)
	if desiredMemberCount < 3 {
		return false, nil
	}
	if len(versionMismatch) != 0 {
		return false, nil
	}
	if quarantinedMembers != 0 {
		return false, nil
	}
	if len(clusterState.members) != desiredMemberCount {
		return false, nil
	}
	if len(clusterState.healthyMembers) < quorumSize(len(clusterState.members)) {
		klog.Infof("empty disk replacement recovery is waiting for healthy quorum: healthy=%d members=%d", len(clusterState.healthyMembers), len(clusterState.members))
		return false, nil
	}

	candidate := m.findDiskReplacementCandidate(clusterState)
	if candidate == nil {
		return false, nil
	}

	if !m.diskReplacementCandidatePastDeadline(candidate, time.Now()) {
		return false, nil
	}

	if !m.hasEtcdLeader(ctx, clusterState) {
		klog.Infof("empty disk replacement candidate %s is waiting for an etcd leader", candidate)
		return false, nil
	}

	if _, err := m.doClusterBackup(ctx, clusterSpec, clusterState); err != nil {
		return false, fmt.Errorf("failed to backup before replacing member %q: %v", candidate.member.Name, err)
	}

	klog.Infof("removing stale etcd member for empty disk replacement: %s", candidate)
	if err := clusterState.etcdRemoveMember(ctx, candidate.member); err != nil {
		return false, fmt.Errorf("failed to remove stale member %q for empty disk replacement peer %q: %v", candidate.member, candidate.peerID, err)
	}

	return true, nil
}

func (m *EtcdController) diskReplacementCandidatePastDeadline(candidate *diskReplacementCandidate, now time.Time) bool {
	// peerState is keyed by stringified etcd member ID cast to PeerId (see controller.go's main loop);
	// we use the stale member ID here to look up health history for the member being replaced.
	peerState := m.peerState[privateapi.PeerId(candidate.memberID)]
	if peerState == nil {
		klog.Warningf("empty disk replacement candidate %s has no health history for stale etcd member ID %q; waiting", candidate, candidate.memberID)
		return false
	}
	age := now.Sub(peerState.lastEtcdHealthy)
	if age < removeUnhealthyDeadline {
		klog.Infof("empty disk replacement candidate %s is waiting for stale member unhealthy deadline %s (currently %s)", candidate, removeUnhealthyDeadline, age)
		return false
	}
	return true
}

func (m *EtcdController) findDiskReplacementCandidate(clusterState *etcdClusterState) *diskReplacementCandidate {
	byName := make(map[string][]etcdMemberRef)
	for id, member := range clusterState.members {
		if member.Name == "" {
			continue
		}
		byName[member.Name] = append(byName[member.Name], etcdMemberRef{id: id, member: member})
	}

	var candidates []diskReplacementCandidate
	for peerID, peer := range clusterState.peers {
		if peer == nil || peer.info == nil || !peer.info.DiskEmpty {
			continue
		}
		if peer.info.NodeConfiguration == nil {
			klog.Warningf("empty disk peer %q has no node configuration; not treating it as a replacement", peerID)
			continue
		}
		if peer.info.EtcdState != nil && peer.info.EtcdState.Cluster != nil {
			klog.Warningf("empty disk peer %q already has etcd cluster state; not treating it as a replacement", peerID)
			continue
		}

		name := peer.info.NodeConfiguration.Name
		if name == "" {
			klog.Warningf("empty disk peer %q has empty node name; not treating it as a replacement", peerID)
			continue
		}

		refs := byName[name]
		if len(refs) > 1 {
			klog.Warningf("empty disk peer %q matches multiple etcd members named %q; skipping this candidate", peerID, name)
			continue
		}
		if len(refs) == 0 {
			continue
		}
		ref := refs[0]
		if clusterState.healthyMembers[ref.id] != nil {
			klog.Infof("empty disk peer %q matches healthy etcd member name %q; not treating it as a replacement", peerID, name)
			continue
		}

		candidates = append(candidates, diskReplacementCandidate{
			peerID:   peerID,
			peer:     peer,
			memberID: ref.id,
			member:   ref.member,
		})
	}

	if len(candidates) > 1 {
		klog.Warningf("found multiple empty disk replacement candidates (%s); replacement recovery is ambiguous and will no-op", formatDiskReplacementCandidates(candidates))
		return nil
	}
	if len(candidates) == 0 {
		return nil
	}
	return &candidates[0]
}

func formatDiskReplacementCandidates(candidates []diskReplacementCandidate) string {
	var parts []string
	for _, candidate := range candidates {
		parts = append(parts, candidate.String())
	}
	return strings.Join(parts, ", ")
}

func (m *EtcdController) hasEtcdLeader(ctx context.Context, clusterState *etcdClusterState) bool {
	for id, member := range clusterState.healthyMembers {
		etcdClient, err := clusterState.newEtcdClient(member)
		if err != nil {
			klog.Warningf("unable to build client for healthy member %s while checking leader: %v", id, err)
			continue
		}

		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		leaderID, err := etcdClient.LeaderID(checkCtx)
		cancel()
		etcdclient.LoggedClose(etcdClient)
		if err != nil {
			klog.Warningf("unable to check leader on healthy member %s: %v", id, err)
			continue
		}
		if leaderID != "" {
			return true
		}
		klog.Infof("healthy member %s reported no etcd leader", id)
	}

	return false
}
