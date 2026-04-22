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

package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
)

var _ discovery.Interface = &AzureVolumes{}

// Poll returns etcd nodes keyed by their IDs.
func (a *AzureVolumes) Poll() (map[string]discovery.Node, error) {
	vs, err := a.FindVolumes()
	if err != nil {
		return nil, fmt.Errorf("error finding volumes: %w", err)
	}

	ctx := context.TODO()
	nodes := map[string]discovery.Node{}
	for _, v := range vs {
		if v.AttachedTo == "" {
			continue
		}

		rid, err := arm.ParseResourceID(v.AttachedTo)
		if err != nil {
			klog.Warningf("error parsing AttachedTo resource ID %q: %v", v.AttachedTo, err)
			continue
		}

		// Extract the VMSS name from the parent resource and the
		// instance ID from the VM name suffix (<vmssName>_<instanceID>).
		vmssName := rid.Parent.Name
		instanceID := vmInstanceID(rid.Name)

		endpoints, err := a.endpointsForVMSSVM(ctx, vmssName, instanceID)
		if err != nil {
			return nil, fmt.Errorf("error getting endpoints for %s: %w", v.AttachedTo, err)
		}

		// We use the etcd node ID as the persistent
		// identifier because the data determines who we are.
		nodes[v.EtcdName] = discovery.Node{
			ID:        v.EtcdName,
			Endpoints: endpoints,
		}
	}

	return nodes, nil
}

// endpointsForVMSSVM returns the private IP endpoints for a specific VMSS VM instance.
func (a *AzureVolumes) endpointsForVMSSVM(ctx context.Context, vmssName, instanceID string) ([]discovery.NodeEndpoint, error) {
	ifaces, err := a.client.listVMSSVMNetworkInterfaces(ctx, vmssName, instanceID)
	if err != nil {
		return nil, err
	}
	var endpoints []discovery.NodeEndpoint
	for _, iface := range ifaces {
		if iface == nil || iface.Properties == nil {
			continue
		}
		for _, ipConfig := range iface.Properties.IPConfigurations {
			if ipConfig == nil || ipConfig.Properties == nil || ipConfig.Properties.PrivateIPAddress == nil {
				continue
			}
			endpoints = append(endpoints, discovery.NodeEndpoint{IP: *ipConfig.Properties.PrivateIPAddress})
		}
	}
	return endpoints, nil
}

// vmInstanceID extracts the VMSS instance ID from a VM name.
// Azure VMSS VMs are named <vmssName>_<instanceID>.
func vmInstanceID(vmName string) string {
	if idx := strings.LastIndex(vmName, "_"); idx >= 0 {
		return vmName[idx+1:]
	}
	return vmName
}
