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

package linode

import (
	"context"
	"fmt"
	"strconv"

	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
)

// LinodeVolumes also allows the discovery of peer nodes.
var _ discovery.Interface = &LinodeVolumes{}

// Poll returns all etcd cluster peers.
func (a *LinodeVolumes) Poll() (map[string]discovery.Node, error) {
	peers := make(map[string]discovery.Node)

	klog.V(2).Infof("Discovering peers with volumes matching tags: %v", a.matchPeerTags)
	matchingVolumes, err := a.findMatchingVolumes(false, a.matchPeerTags)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching volumes: %w", err)
	}

	for _, volume := range matchingVolumes {
		if volume.AttachedTo == "" {
			// Volume doesn't have an instance attached yet
			continue
		}

		linodeID, err := strconv.Atoi(volume.AttachedTo)
		if err != nil {
			return nil, fmt.Errorf("failed to parse attached instance id %q for volume %s: %w", volume.AttachedTo, volume.ProviderID, err)
		}

		ip, err := a.getPrivateIPv4(context.TODO(), linodeID)
		if err != nil {
			return nil, fmt.Errorf("failed to get private IP for instance %d: %w", linodeID, err)
		}

		klog.V(2).Infof("Discovered volume %s attached to instance %d", volume.ProviderID, linodeID)
		node := discovery.Node{
			ID:        volume.EtcdName,
			Endpoints: []discovery.NodeEndpoint{{IP: ip}},
		}
		peers[node.ID] = node
	}

	return peers, nil
}
