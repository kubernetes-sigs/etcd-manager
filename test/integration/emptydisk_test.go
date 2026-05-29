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

package integration

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"k8s.io/klog/v2"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/etcdclient"
	"sigs.k8s.io/etcd-manager/test/integration/harness"
)

func TestEmptyDiskReplacementRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := context.TODO()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()

	h := harness.NewTestHarness(t, ctx)
	h.SeedNewCluster(&protoetcd.ClusterSpec{MemberCount: 3, EtcdVersion: "3.5.7"})
	defer h.Close()

	n1 := h.NewNode("127.0.0.1")
	n2 := h.NewNode("127.0.0.2")
	n3 := h.NewNode("127.0.0.3")
	nodes := []*harness.TestHarnessNode{n1, n2, n3}
	for _, node := range nodes {
		go node.Run()
	}

	members := waitForMemberCount(ctx, h, n1, 3, time.Minute)
	waitForAllNodesHealthy(ctx, h, nodes, time.Minute)

	key := "/testing/empty-disk-replacement"
	value := time.Now().String()
	if err := n1.Put(ctx, key, value); err != nil {
		t.Fatalf("error writing key %q: %v", key, err)
	}

	victim := highestPeerIDNode(t, nodes)
	victimPeerID, err := victim.PeerID()
	if err != nil {
		t.Fatalf("error reading victim peer id: %v", err)
	}
	if memberID := memberIDForName(members, string(victimPeerID)); memberID == "" {
		t.Fatalf("could not find etcd member for victim peer %q in %v", victimPeerID, members)
	}

	klog.Infof("replacing node %s with peer id %q using an empty disk", victim.Address, victimPeerID)
	if _, err := victim.ReplaceWithEmptyDiskSameIdentity(); err != nil {
		t.Fatalf("failed to replace victim disk: %v", err)
	}
	go victim.Run()

	members = waitForMemberCount(ctx, h, victim, 3, 4*time.Minute)
	if memberID := memberIDForName(members, string(victimPeerID)); memberID == "" {
		t.Fatalf("replacement peer %q was not present in etcd members %v", victimPeerID, members)
	}

	h.WaitFor(time.Minute, "replacement can read previously written key", func() error {
		actual, err := victim.GetQuorum(ctx, key)
		if err != nil {
			return err
		}
		if actual != value {
			return fmt.Errorf("unexpected value for %q: got %q, want %q", key, actual, value)
		}
		return nil
	})
}

func waitForAllNodesHealthy(ctx context.Context, h *harness.TestHarness, nodes []*harness.TestHarnessNode, timeout time.Duration) {
	h.WaitFor(timeout, "all nodes list members", func() error {
		for _, node := range nodes {
			if _, err := node.ListMembers(ctx); err != nil {
				return fmt.Errorf("node %s did not list members: %w", node.Address, err)
			}
		}
		return nil
	})
}

func waitForMemberCount(ctx context.Context, h *harness.TestHarness, node *harness.TestHarnessNode, count int, timeout time.Duration) []*etcdclient.EtcdProcessMember {
	var members []*etcdclient.EtcdProcessMember
	h.WaitFor(timeout, fmt.Sprintf("node %s sees %d members", node.Address, count), func() error {
		var err error
		members, err = node.ListMembers(ctx)
		if err != nil {
			return err
		}
		if len(members) != count {
			return fmt.Errorf("got %d members, want %d: %v", len(members), count, members)
		}
		return nil
	})
	return members
}

func highestPeerIDNode(t *testing.T, nodes []*harness.TestHarnessNode) *harness.TestHarnessNode {
	t.Helper()

	type nodeWithPeerID struct {
		node   *harness.TestHarnessNode
		peerID string
	}

	var withPeerIDs []nodeWithPeerID
	for _, node := range nodes {
		peerID, err := node.PeerID()
		if err != nil {
			t.Fatalf("error reading peer id for node %s: %v", node.Address, err)
		}
		withPeerIDs = append(withPeerIDs, nodeWithPeerID{node: node, peerID: string(peerID)})
	}
	sort.Slice(withPeerIDs, func(i, j int) bool {
		return withPeerIDs[i].peerID < withPeerIDs[j].peerID
	})
	return withPeerIDs[len(withPeerIDs)-1].node
}

func memberIDForName(members []*etcdclient.EtcdProcessMember, name string) string {
	for _, member := range members {
		if member.Name == name {
			return member.ID
		}
	}
	return ""
}
