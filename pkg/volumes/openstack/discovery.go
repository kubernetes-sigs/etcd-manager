/*
Copyright 2019 The Kubernetes Authors.

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

package openstack

import (
	"context"

	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

// OpenstackVolumes also allows us to discover our peer nodes
var _ discovery.Interface = &OpenstackVolumes{}

func (os *OpenstackVolumes) Poll() (map[string]discovery.Node, error) {
	ctx := context.TODO()

	allVolumes, err := os.findVolumes(ctx, false)
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]discovery.Node)
	instanceToVolumeMap := make(map[string]*volumes.Volume)
	for _, v := range allVolumes {
		if v.AttachedTo != "" {
			instanceToVolumeMap[v.AttachedTo] = v
		}
	}
	for i, volume := range instanceToVolumeMap {
		mc := NewMetricContext("server", "get")
		server, err := servers.Get(ctx, os.computeClient, i).Extract()
		if mc.ObserveRequest(err) != nil {
			klog.Warningf("Could not find server with id '%s': %v", i, err)
			continue
		}

		// We use the etcd node ID as the persistent identifier, because the data determines who we are
		node := discovery.Node{
			ID: volume.EtcdName,
		}
		address, err := GetServerFixedIP(server.Addresses, server.Name, os.networkCIDR)
		if err != nil {
			klog.Warningf("Could not find servers fixed ip %s: %v", server.Name, err)
			continue
		}
		node.Endpoints = append(node.Endpoints, discovery.NodeEndpoint{IP: address})
		nodes[node.ID] = node
	}

	return nodes, nil
}
