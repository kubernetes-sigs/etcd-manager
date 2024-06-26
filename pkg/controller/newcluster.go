/*
Copyright 2020 The Kubernetes Authors.

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

	"github.com/golang/protobuf/proto"
	"k8s.io/klog/v2"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/urls"
)

// createNewCluster starts a new etcd cluster.
// It tries to identify a quorum of nodes, and if found will instruct each to join the cluster.
func (m *EtcdController) createNewCluster(ctx context.Context, clusterState *etcdClusterState, clusterSpec *protoetcd.ClusterSpec) (bool, error) {
	desiredMemberCount := int(clusterSpec.MemberCount)
	desiredQuorumSize := quorumSize(desiredMemberCount)

	if len(clusterState.peers) < desiredQuorumSize {
		klog.Infof("Insufficient peers to form a quorum %d, won't proceed", desiredQuorumSize)
		return false, nil
	}

	if len(clusterState.peers) < desiredMemberCount {
		// TODO: We should relax this, but that requires etcd to support an explicit quorum setting, or we can create dummy entries

		// But ... as a special case, we can allow it through if the quorum size is the same (i.e. one less than desired)
		if quorumSize(len(clusterState.peers)) == desiredQuorumSize {
			klog.Infof("Fewer peers (%d) than desired members (%d), but quorum size is the same, so will proceed", len(clusterState.peers), desiredMemberCount)
		} else {
			klog.Infof("Insufficient peers to form full cluster %d, won't proceed", desiredQuorumSize)
			return false, nil
		}
	}

	clusterToken := randomToken()

	var proposal []*etcdClusterPeerInfo
	for _, peer := range clusterState.peers {
		proposal = append(proposal, peer)
		if len(proposal) == desiredMemberCount {
			// We have identified enough members to form a cluster
			break
		}
	}

	if len(proposal) < desiredMemberCount && quorumSize(len(proposal)) < quorumSize(desiredMemberCount) {
		klog.Fatalf("Need to add dummy peers to force quorum size :-(")
	}

	// Build the proposed nodes and the proposed member map

	var proposedNodes []*protoetcd.EtcdNode
	memberMap := &protoetcd.MemberMap{}
	for _, p := range proposal {
		node := proto.Clone(p.info.NodeConfiguration).(*protoetcd.EtcdNode)

		if m.disableEtcdTLS {
			node.PeerUrls = urls.RewriteScheme(node.PeerUrls, "https://", "http://")
			node.ClientUrls = urls.RewriteScheme(node.ClientUrls, "https://", "http://")
			node.TlsEnabled = false
		} else {
			node.PeerUrls = urls.RewriteScheme(node.PeerUrls, "http://", "https://")
			node.ClientUrls = urls.RewriteScheme(node.ClientUrls, "http://", "https://")
			node.TlsEnabled = true
		}

		proposedNodes = append(proposedNodes, node)

		memberInfo := &protoetcd.MemberMapInfo{
			Name: node.Name,
		}

		if m.dnsSuffix != "" {
			dnsSuffix := m.dnsSuffix
			if !strings.HasPrefix(dnsSuffix, ".") {
				dnsSuffix = "." + dnsSuffix
			}
			memberInfo.Dns = node.Name + dnsSuffix
		}

		if p.peer.info == nil {
			return false, fmt.Errorf("no info for peer %v", p)
		}

		for _, a := range p.peer.info.Endpoints {
			ip := a
			memberInfo.Addresses = append(memberInfo.Addresses, ip)
		}

		memberMap.Members = append(memberMap.Members, memberInfo)
	}

	// Stop any running etcd
	for _, p := range clusterState.peers {
		peer := p.peer

		if p.info != nil && p.info.EtcdState != nil {
			request := &protoetcd.StopEtcdRequest{
				Header: m.buildHeader(),
			}
			response, err := peer.rpcStopEtcd(ctx, request)
			if err != nil {
				return false, fmt.Errorf("error stopping etcd peer %q: %v", peer.Id, err)
			}
			klog.Infof("stopped etcd on peer %q: %v", peer.Id, response)
		}
	}

	// Broadcast the proposed member map so everyone is consistent
	if errors := m.broadcastMemberMap(ctx, clusterState, memberMap); len(errors) != 0 {
		return false, fmt.Errorf("unable to broadcast member map: %v", errors)
	}

	klog.Infof("starting new etcd cluster with %s", proposal)

	for _, p := range proposal {
		// Note that we may send the message to ourselves
		joinClusterRequest := &protoetcd.JoinClusterRequest{
			Header:       m.buildHeader(),
			Phase:        protoetcd.Phase_PHASE_PREPARE,
			ClusterToken: clusterToken,
			EtcdVersion:  clusterSpec.EtcdVersion,
			Nodes:        proposedNodes,
		}

		joinClusterResponse, err := p.peer.rpcJoinCluster(ctx, joinClusterRequest)
		if err != nil {
			// TODO: Send a CANCEL message for anything PREPAREd?  (currently we rely on a slow timeout)
			return false, fmt.Errorf("error from JoinClusterRequest (prepare) from peer %q: %v", p.peer, err)
		}
		klog.V(2).Infof("JoinClusterResponse: %s", joinClusterResponse)
	}

	for _, p := range proposal {
		// Note that we may send the message to ourselves
		joinClusterRequest := &protoetcd.JoinClusterRequest{
			Header:       m.buildHeader(),
			Phase:        protoetcd.Phase_PHASE_INITIAL_CLUSTER,
			ClusterToken: clusterToken,
			EtcdVersion:  clusterSpec.EtcdVersion,
			Nodes:        proposedNodes,
		}

		joinClusterResponse, err := p.peer.rpcJoinCluster(ctx, joinClusterRequest)
		if err != nil {
			// TODO: Send a CANCEL message for anything PREPAREd?  (currently we rely on a slow timeout)
			return false, fmt.Errorf("error from JoinClusterRequest from peer %q: %v", p.peer, err)
		}
		klog.V(2).Infof("JoinClusterResponse: %s", joinClusterResponse)
	}

	// Write cluster spec to etcd
	if err := m.writeClusterSpec(ctx, clusterState, clusterSpec); err != nil {
		return false, err
	}

	return true, nil
}
