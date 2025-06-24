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

package integration

import (
	"context"
	"strconv"
	"testing"
	"time"

	"k8s.io/klog/v2"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/test/integration/harness"
)

func TestClusterDataPersists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	defer cancel()

	h := harness.NewTestHarness(t, ctx)
	h.SeedNewCluster(&protoetcd.ClusterSpec{MemberCount: 1, EtcdVersion: "3.5.7"})
	defer h.Close()

	n1 := h.NewNode("127.0.0.1")
	go n1.Run()

	n1.WaitForListMembers(20 * time.Second)

	key := "/testing/hello"

	value := "world"

	err := n1.Put(ctx, key, value)
	if err != nil {
		t.Fatalf("error writing key %q: %v", key, err)
	}

	{
		actual, err := n1.GetQuorum(ctx, key)
		if err != nil {
			t.Fatalf("error reading key %q: %v", key, err)
		}
		if actual != value {
			t.Fatalf("could not read back key %q: %q vs %q", key, actual, value)
		}
	}

	// We should be able to shut down the node, restart it and the data should be there
	if err := n1.Close(); err != nil {
		t.Fatalf("failed to stop node 1: %v", err)
	}

	klog.Infof("restarting node %v", n1)
	go n1.Run()

	n1.WaitForListMembers(time.Second * 20)

	{
		actual, err := n1.GetQuorum(ctx, key)
		if err != nil {
			t.Fatalf("error rereading key %q: %v", key, err)
		}
		if actual != value {
			t.Fatalf("could not reread key %q: %q vs %q", key, actual, value)
		}
	}

	cancel()
	h.Close()
}

func TestHAReadWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	defer cancel()

	h := harness.NewTestHarness(t, ctx)
	h.SeedNewCluster(&protoetcd.ClusterSpec{MemberCount: 3, EtcdVersion: "3.5.7"})
	defer h.Close()

	n1 := h.NewNode("127.0.0.1")
	go n1.Run()
	n2 := h.NewNode("127.0.0.2")
	go n2.Run()

	key := "/testing/hareadwrite"

	value := "write-on-one-read-on-another"

	// Wait for cluster to achieve quorum
	n1.WaitForQuorumRead(ctx, time.Second*30)

	err := n1.Put(ctx, key, value)
	if err != nil {
		t.Fatalf("error writing key %q: %v", key, err)
	}

	// We bring up a third node
	klog.Infof("starting new node %v", n1)
	n3 := h.NewNode("127.0.0.3")
	go n3.Run()

	n3.WaitForListMembers(time.Second * 20)

	// We now shut down the node we wrote it on, but it should be readable on the third node
	if err := n1.Close(); err != nil {
		t.Fatalf("failed to stop node 1: %v", err)
	}

	// After a leader loss, quorum reads fail until etcd recovers
	n3.WaitForQuorumRead(ctx, time.Second*30)

	{
		actual, err := n3.GetQuorum(ctx, key)
		if err != nil {
			t.Fatalf("error rereading key (quorum) %q: %v", key, err)
		}
		if actual != value {
			t.Fatalf("could not reread key (quorum) %q: %q vs %q", key, actual, value)
		}
	}

	// Once we've done a quorum read, we should be able to do a local read
	{
		actual, err := n3.GetLocal(ctx, key)
		if err != nil {
			t.Fatalf("error rereading key (local) %q: %v", key, err)
		}
		if actual != value {
			t.Fatalf("could not reread key (local) %q: %q vs %q", key, actual, value)
		}
	}

	cancel()
	h.Close()
}

// TestHARecovery tests that after a full shutdown of all nodes, we still have data
func TestHARecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := context.TODO()
	ctx, cancel := context.WithCancel(ctx)

	defer cancel()

	h := harness.NewTestHarness(t, ctx)
	h.SeedNewCluster(&protoetcd.ClusterSpec{MemberCount: 3, EtcdVersion: "3.5.7"})
	defer h.Close()

	n1 := h.NewNode("127.0.0.1")
	go n1.Run()
	n2 := h.NewNode("127.0.0.2")
	go n2.Run()
	n3 := h.NewNode("127.0.0.3")
	go n3.Run()

	n1.WaitForQuorumRead(ctx, 30*time.Second)

	key := "/testing/harecovery-" + strconv.FormatInt(time.Now().Unix(), 10)
	value := time.Now().String()

	err := n1.Put(ctx, key, value)
	if err != nil {
		t.Fatalf("error writing key %q: %v", key, err)
	}

	// We now shut down all 3 nodes
	if err := n1.Close(); err != nil {
		t.Fatalf("failed to stop node 1: %v", err)
	}
	if err := n2.Close(); err != nil {
		t.Fatalf("failed to stop node 2: %v", err)
	}
	if err := n3.Close(); err != nil {
		t.Fatalf("failed to stop node 3: %v", err)
	}

	// We bring up nodes 2 and 3
	klog.Infof("restarting node %v", n2)
	go n2.Run()

	klog.Infof("restarting node %v", n3)
	go n3.Run()

	// Wait for n3 node to be running (but not necessarily happy)
	n3.WaitForListMembers(20 * time.Second)

	// After a leader loss, quorum reads fail until etcd recovers
	n3.WaitForQuorumRead(ctx, time.Second*30)

	{
		actual, err := n3.GetQuorum(ctx, key)
		if err != nil {
			t.Fatalf("error rereading key (quorum) %q: %v", key, err)
		}
		if actual != value {
			t.Fatalf("could not reread key %q: %q vs %q", key, actual, value)
		}
	}

	cancel()
	h.Close()
}
