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

package privateapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
)

// fakeDiscovery is a discovery.Interface that returns a fixed set of nodes.
type fakeDiscovery struct {
	nodes map[string]discovery.Node
	err   error
}

func (f *fakeDiscovery) Poll() (map[string]discovery.Node, error) {
	return f.nodes, f.err
}

func newTestServer(t *testing.T, ctx context.Context, disco discovery.Interface) *Server {
	t.Helper()

	myInfo := &PeerInfo{
		Id:        "self",
		Endpoints: []string{"127.0.0.1:8000"},
	}
	// dnsProvider/dnsSuffix and TLS configs are unused: no DNS suffix, connections are insecure.
	s, err := NewServer(ctx, myInfo, nil, disco, 8000, nil, "", nil, time.Minute)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	return s
}

// TestServerSeedsSelfIntoPeers verifies Peers() includes self immediately after construction.
func TestServerSeedsSelfIntoPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := newTestServer(t, ctx, &fakeDiscovery{nodes: map[string]discovery.Node{}})

	peers := s.Peers()
	if len(peers) != 1 {
		t.Fatalf("expected exactly 1 peer (self), got %d: %v", len(peers), peers)
	}
	if PeerId(peers[0].Id) != s.MyPeerId() {
		t.Fatalf("expected peer to be self %q, got %q", s.MyPeerId(), peers[0].Id)
	}
}

// TestDiscoveryAddsOtherPeers checks that seeding self still lets discovery add other peers.
func TestDiscoveryAddsOtherPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disco := &fakeDiscovery{
		nodes: map[string]discovery.Node{
			"self":   {ID: "self", Endpoints: []discovery.NodeEndpoint{{IP: "127.0.0.1", Port: 8000}}},
			"other1": {ID: "other1", Endpoints: []discovery.NodeEndpoint{{IP: "127.0.0.2", Port: 8000}}},
			"other2": {ID: "other2", Endpoints: []discovery.NodeEndpoint{{IP: "127.0.0.3", Port: 8000}}},
		},
	}
	s := newTestServer(t, ctx, disco)

	if err := s.runDiscoveryOnce(); err != nil {
		t.Fatalf("runDiscoveryOnce failed: %v", err)
	}

	hasPeer := func(id PeerId) bool {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		return s.peers[id] != nil
	}
	for _, id := range []PeerId{"self", "other1", "other2"} {
		if !hasPeer(id) {
			t.Errorf("expected peer %q to be tracked after discovery", id)
		}
	}
}

// TestRunDiscoveryOnceErrorKeepsSelf verifies that a failed first discovery poll (the Azure case
// where the data disk isn't visible yet) keeps self in the peer list and reports the error.
func TestRunDiscoveryOnceErrorKeepsSelf(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disco := &fakeDiscovery{err: fmt.Errorf("no data disk found")}
	s := newTestServer(t, ctx, disco)

	if err := s.runDiscoveryOnce(); err == nil {
		t.Fatal("expected error from runDiscoveryOnce when discovery poll fails")
	}

	peers := s.Peers()
	if len(peers) != 1 || PeerId(peers[0].Id) != s.MyPeerId() {
		t.Fatalf("expected self to remain the only peer after failed discovery, got %v", peers)
	}
}
