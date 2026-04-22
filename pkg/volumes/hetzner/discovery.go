/*
Copyright 2022 The Kubernetes Authors.

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

package hetzner

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
)

// HetznerVolumes also allows the discovery of peer nodes
var _ discovery.Interface = &HetznerVolumes{}

// Poll returns all etcd cluster peers.
func (a *HetznerVolumes) Poll() (map[string]discovery.Node, error) {
	peers := make(map[string]discovery.Node)

	klog.V(2).Infof("Discovering peers with volumes matching labels: %v", a.matchPeerTags)
	matchingVolumes, err := getMatchingVolumes(a.hcloudClient, a.matchPeerTags)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching volumes: %w", err)
	}

	// Fetch all servers once to avoid hitting Hetzner's 3600 req/hour rate limit
	// (see: https://docs.hetzner.cloud/#rate-limiting).
	allServers, err := a.hcloudClient.Server.All(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("failed to list servers: %w", err)
	}
	serversByID := make(map[int64]*hcloud.Server, len(allServers))
	for _, server := range allServers {
		serversByID[server.ID] = server
	}

	for _, volume := range matchingVolumes {
		if volume.Server == nil {
			// Volume doesn't have a server attached yet
			continue
		}
		serverID := volume.Server.ID

		server, ok := serversByID[serverID]
		if !ok {
			klog.Warningf("skipping volume %s(%d): server %d not found", volume.Name, volume.ID, serverID)
			continue
		}

		if len(server.PrivateNet) == 0 {
			klog.Warningf("skipping volume %s(%d): server %d has no private net", volume.Name, volume.ID, serverID)
			continue
		}
		serverPrivateIP := server.PrivateNet[0].IP

		klog.V(2).Infof("Discovered volume %s(%d) attached to server %s(%d)", volume.Name, volume.ID, server.Name, serverID)
		// We use the etcd node ID as the persistent identifier, because the data determines who we are
		node := discovery.Node{
			ID:        "vol-" + strconv.FormatInt(volume.ID, 10),
			Endpoints: []discovery.NodeEndpoint{{IP: serverPrivateIP.String()}},
		}
		peers[node.ID] = node
	}

	return peers, nil
}
