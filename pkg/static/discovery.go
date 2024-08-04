/*
Copyright 2024 The Kubernetes Authors.

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

package static

import (
	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
)

// StaticDiscovery implements discovery.Interface using a fixed configuration
type StaticDiscovery struct {
	config *Config
}

var _ discovery.Interface = &StaticDiscovery{}

func NewStaticDiscovery(config *Config) *StaticDiscovery {
	d := &StaticDiscovery{
		config: config,
	}
	return d
}

func (d *StaticDiscovery) Poll() (map[string]discovery.Node, error) {
	nodes := make(map[string]discovery.Node)

	for _, configNode := range d.config.Nodes {
		node := discovery.Node{}
		node.ID = configNode.ID
		for _, ip := range configNode.IP {
			node.Endpoints = append(node.Endpoints, discovery.NodeEndpoint{
				IP: ip,
			})
		}
		nodes[node.ID] = node
	}

	klog.Infof("static discovery poll => %+v", nodes)
	return nodes, nil
}
