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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	compute "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	network "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
)

type managedDisk struct {
	ID string `json:"id"`
}

type dataDisk struct {
	Name        string       `json:"name"`
	Lun         string       `json:"lun"`
	ManagedDisk *managedDisk `json:"managedDisk"`
}

type storageProfile struct {
	DataDisks []*dataDisk `json:"dataDisks"`
}

type instanceComputeMetadata struct {
	Name              string          `json:"name"`
	ResourceGroupName string          `json:"resourceGroupName"`
	VMScaleSetName    string          `json:"vmScaleSetName"`
	SubscriptionID    string          `json:"subscriptionId"`
	StorageProfile    *storageProfile `json:"storageProfile"`
}

type ipAddress struct {
	PrivateIPAddress string `json:"privateIpAddress"`
	PublicIPAddress  string `json:"publicIpAddress"`
}

type ipv4Interface struct {
	IPAddresses []*ipAddress `json:"ipAddress"`
}

type networkInterface struct {
	IPv4 *ipv4Interface `json:"ipv4"`
}

type instanceNetworkMetadata struct {
	Interfaces []*networkInterface `json:"interface"`
}

type instanceMetadata struct {
	Compute *instanceComputeMetadata `json:"compute"`
	Network *instanceNetworkMetadata `json:"network"`
}

type client struct {
	metadata          *instanceMetadata
	scaleSets         *compute.VirtualMachineScaleSetsClient
	scaleSetVMs       *compute.VirtualMachineScaleSetVMsClient
	disks             *compute.DisksClient
	networkInterfaces *network.InterfacesClient
}

func newClient() (*client, error) {
	m, err := queryInstanceMetadata()
	if err != nil {
		return nil, fmt.Errorf("error querying instance metadata: %w", err)
	}
	if m.Compute.SubscriptionID == "" {
		return nil, fmt.Errorf("empty subscription name")
	}
	if m.Compute.ResourceGroupName == "" {
		return nil, fmt.Errorf("empty resource group name")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating identity: %w", err)
	}

	scaleSets, err := compute.NewVirtualMachineScaleSetsClient(m.Compute.SubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating vmss client: %w", err)
	}

	scaleSetVMs, err := compute.NewVirtualMachineScaleSetVMsClient(m.Compute.SubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating vmssvm client: %w", err)
	}

	disks, err := compute.NewDisksClient(m.Compute.SubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating disks client: %w", err)
	}

	networkInterfaces, err := network.NewInterfacesClient(m.Compute.SubscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating network interfaces client: %w", err)
	}

	return &client{
		metadata:          m,
		scaleSets:         scaleSets,
		scaleSetVMs:       scaleSetVMs,
		disks:             disks,
		networkInterfaces: networkInterfaces,
	}, nil
}

func (c *client) name() string {
	return c.metadata.Compute.Name
}

func (c *client) vmScaleSetInstanceID() string {
	l := strings.Split(c.metadata.Compute.Name, "_")
	return l[len(l)-1]
}

func (c *client) resourceGroupName() string {
	return c.metadata.Compute.ResourceGroupName
}

func (c *client) vmScaleSetName() string {
	return c.metadata.Compute.VMScaleSetName
}

func (c *client) dataDisks() []*dataDisk {
	return c.metadata.Compute.StorageProfile.DataDisks
}

func (c *client) refreshMetadata() error {
	m, err := queryInstanceMetadata()
	if err != nil {
		return fmt.Errorf("error querying instance metadata: %w", err)
	}
	c.metadata = m
	return nil
}

func (c *client) localIP() net.IP {
	for _, iface := range c.metadata.Network.Interfaces {
		if iface.IPv4 == nil {
			continue
		}
		for _, ipAddr := range iface.IPv4.IPAddresses {
			if a := ipAddr.PrivateIPAddress; a != "" {
				return net.ParseIP(a)
			}
		}
	}
	return nil
}

func (c *client) listVMScaleSetVMs(ctx context.Context) ([]*compute.VirtualMachineScaleSetVM, error) {
	var l []*compute.VirtualMachineScaleSetVM
	pager := c.scaleSetVMs.NewListPager(c.resourceGroupName(), c.vmScaleSetName(), nil)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			var respErr *azcore.ResponseError
			if errors.As(err, &respErr) && respErr.ErrorCode == "ResourceGroupNotFound" {
				return nil, nil
			}
			return nil, fmt.Errorf("listing VMSSs: %w", err)
		}
		l = append(l, resp.Value...)
	}
	return l, nil
}

func (c *client) getVMScaleSetVM(ctx context.Context, instanceID string) (*compute.VirtualMachineScaleSetVM, error) {
	opts := &compute.VirtualMachineScaleSetVMsClientGetOptions{
		Expand: to.Ptr(compute.InstanceViewTypesInstanceView),
	}
	inst, err := c.scaleSetVMs.Get(ctx, c.resourceGroupName(), c.vmScaleSetName(), instanceID, opts)
	if err != nil {
		return nil, fmt.Errorf("getting VMSS VMs info: %w", err)
	}
	return &inst.VirtualMachineScaleSetVM, nil
}

func (c *client) updateVMScaleSetVM(ctx context.Context, instanceID string, parameters compute.VirtualMachineScaleSetVM) error {
	future, err := c.scaleSetVMs.BeginUpdate(ctx, c.resourceGroupName(), c.vmScaleSetName(), instanceID, parameters, nil)
	if err != nil {
		return fmt.Errorf("error updating VM Scale Set VM: %w", err)
	}
	if _, err := future.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("error waiting for VM Scale Set VM update completion: %w", err)
	}
	return nil
}

func (c *client) listDisks(ctx context.Context) ([]*compute.Disk, error) {
	var l []*compute.Disk
	pager := c.disks.NewListPager(nil)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing disks: %w", err)
		}
		l = append(l, resp.Value...)
	}
	return l, nil
}

func (c *client) listVMSSNetworkInterfaces(ctx context.Context) ([]*network.Interface, error) {
	var l []*network.Interface
	pager := c.networkInterfaces.NewListVirtualMachineScaleSetNetworkInterfacesPager(c.resourceGroupName(), c.vmScaleSetName(), nil)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing network interfaces: %w", err)
		}
		l = append(l, resp.Value...)
	}
	return l, nil
}

// queryInstanceMetadata queries Azure Instance Metadata documented in
// https://docs.microsoft.com/en-us/azure/virtual-machines/windows/instance-metadata-service.
func queryInstanceMetadata() (*instanceMetadata, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://169.254.169.254/metadata/instance", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating a new request: %w", err)
	}
	req.Header.Add("Metadata", "True")

	q := req.URL.Query()
	q.Add("format", "json")
	q.Add("api-version", "2020-06-01")
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request to the metadata server: %w", err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading a response from the metadata server: %w", err)
	}
	metadata, err := unmarshalInstanceMetadata(body)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling metadata: %w", err)
	}
	return metadata, nil
}

func unmarshalInstanceMetadata(data []byte) (*instanceMetadata, error) {
	m := &instanceMetadata{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, err
	}
	return m, nil
}
