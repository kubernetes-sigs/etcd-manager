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
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	compute "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	network "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"sigs.k8s.io/etcd-manager/pkg/privateapi/discovery"
)

func newTestInterface(vmID, ip string) *network.Interface {
	return &network.Interface{
		Properties: &network.InterfacePropertiesFormat{
			VirtualMachine: &network.SubResource{
				ID: to.Ptr(vmID),
			},
			IPConfigurations: []*network.InterfaceIPConfiguration{
				{
					Properties: &network.InterfaceIPConfigurationPropertiesFormat{
						PrivateIPAddress: to.Ptr(ip),
					},
				},
			},
		},
	}
}

func TestPoll(t *testing.T) {
	client := newMockClient()
	a, err := newAzureVolumes("cluster", []string{}, "nameTag", client)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	client.disks = []*compute.Disk{
		{
			Name: to.Ptr("name0"),
			ID:   to.Ptr("did0"),
			Properties: &compute.DiskProperties{
				DiskState: to.Ptr(compute.DiskState("state")),
			},
			ManagedBy: to.Ptr("vm_0"),
		},
		{
			Name: to.Ptr("name1"),
			ID:   to.Ptr("did1"),
			Properties: &compute.DiskProperties{
				DiskState: to.Ptr(compute.DiskState("state")),
			},
			ManagedBy: to.Ptr("vm_1"),
		},
		{
			// Unmanaged disk.
			Name: to.Ptr("name2"),
			ID:   to.Ptr("did2"),
			Properties: &compute.DiskProperties{
				DiskState: to.Ptr(compute.DiskState("state")),
			},
		},
	}
	client.vms = map[string]*compute.VirtualMachineScaleSetVM{
		"0": {
			Name: to.Ptr("vm_0"),
			ID:   to.Ptr("vmid0"),
		},
		"1": {
			Name: to.Ptr("vm_1"),
			ID:   to.Ptr("vmid1"),
		},
		"2": {
			Name: to.Ptr("vm_2"),
			ID:   to.Ptr("vmid2"),
		},
	}
	client.ifaces = []*network.Interface{
		newTestInterface("vmid0", "10.0.0.1"),
		newTestInterface("vmid1", "10.0.0.2"),
		newTestInterface("vmid2", "10.0.0.3"),
	}

	actual, err := a.Poll()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	expected := map[string]discovery.Node{
		"name0": {
			ID: "name0",
			Endpoints: []discovery.NodeEndpoint{
				{
					IP: "10.0.0.1",
				},
			},
		},
		"name1": {
			ID: "name1",
			Endpoints: []discovery.NodeEndpoint{
				{
					IP: "10.0.0.2",
				},
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected %+v, but got +%v", expected, actual)
	}
}
