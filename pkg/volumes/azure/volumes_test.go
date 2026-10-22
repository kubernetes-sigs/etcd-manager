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
	"fmt"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	compute "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

func TestIsDiskForCluster(t *testing.T) {
	volumeTags := []string{"k0", "k1=v1"}
	a, err := newAzureVolumes("cluster", volumeTags, "nameTag", newMockClient())
	if err != nil {
		t.Fatalf("unexpected error %s", err)
	}

	testCases := []struct {
		tags   map[string]*string
		result bool
	}{
		{
			tags:   map[string]*string{},
			result: false,
		},
		{
			tags: map[string]*string{
				"k0":      to.Ptr("any value"),
				"k1":      to.Ptr("v1"),
				"any key": to.Ptr("any value"),
			},
			result: true,
		},
		{
			tags: map[string]*string{
				"k0": to.Ptr("any value"),
			},
			result: false,
		},
		{
			tags: map[string]*string{
				"k1": to.Ptr("v2"),
			},
			result: false,
		},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			d := &compute.Disk{
				Tags: tc.tags,
			}
			if a, e := a.isDiskForCluster(d), tc.result; a != e {
				t.Errorf("expected %t, but got %t", e, a)
			}
		})
	}
}

func TestExtractEtcdName(t *testing.T) {
	diskName := "dname"
	clusterName := "cluster"

	testCases := []struct {
		nameTag  string
		tags     map[string]*string
		etcdName string
	}{
		{
			nameTag:  "",
			tags:     map[string]*string{},
			etcdName: diskName,
		},
		{
			nameTag:  "nameTag",
			tags:     map[string]*string{},
			etcdName: diskName,
		},
		{
			nameTag: "nameTag",
			tags: map[string]*string{
				"nameTag": to.Ptr(""),
			},
			etcdName: diskName,
		},
		{
			nameTag: "nameTag",
			tags: map[string]*string{
				"nameTag": to.Ptr("etcd_name/members"),
			},
			etcdName: clusterName + "-" + "etcd_name",
		},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			a, err := newAzureVolumes("cluster", []string{}, tc.nameTag, newMockClient())
			if err != nil {
				t.Fatalf("unexpected error %s", err)
			}
			d := &compute.Disk{
				Name: to.Ptr(diskName),
				Tags: tc.tags,
			}
			if n := a.extractEtcdName(d); n != tc.etcdName {
				t.Errorf("expected %s, but got %s", tc.etcdName, n)
			}
		})
	}
}

func TestFindAvailableLun(t *testing.T) {
	testCases := []struct {
		dataDisks []*compute.DataDisk
		lun       int32
	}{
		{
			dataDisks: nil,
			lun:       0,
		},
		{
			dataDisks: []*compute.DataDisk{
				{
					Lun: to.Ptr(int32(0)),
				},
			},
			lun: 1,
		},
		{
			dataDisks: []*compute.DataDisk{
				{
					Lun: to.Ptr(int32(0)),
				},
				{
					Lun: to.Ptr(int32(1)),
				},
			},
			lun: 2,
		},
		{
			dataDisks: []*compute.DataDisk{
				{
					Lun: to.Ptr(int32(1)),
				},
				{
					Lun: to.Ptr(int32(3)),
				},
			},
			lun: 4,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			client := newMockClient()
			client.vms[client.vmScaleSetInstanceID()] = &compute.VirtualMachineScaleSetVM{
				Properties: &compute.VirtualMachineScaleSetVMProperties{
					StorageProfile: &compute.StorageProfile{
						DataDisks: tc.dataDisks,
					},
				},
			}
			a, err := newAzureVolumes("cluster", []string{}, "nameTag", client)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			actual, err := a.findAvailableLun()
			if err != nil {
				t.Fatalf("unexpected error %s", err)
			}
			if actual != tc.lun {
				t.Errorf("expected %d, but got %d", tc.lun, actual)
			}
		})
	}
}

func TestAttachVolume(t *testing.T) {
	const (
		diskName = "etcd-1.etcd-main.cluster"
		diskID   = "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/disks/etcd-1.etcd-main.cluster"
	)

	testCases := []struct {
		name      string
		dataDisks []*compute.DataDisk
		lun       int32
	}{
		{
			name: "no data disks",
			lun:  0,
		},
		{
			name: "existing data disk",
			dataDisks: []*compute.DataDisk{
				{
					Lun:          to.Ptr(int32(0)),
					Name:         to.Ptr("etcd-1.etcd-events.cluster"),
					CreateOption: to.Ptr(compute.DiskCreateOptionTypesAttach),
					Caching:      to.Ptr(compute.CachingTypesNone),
					ManagedDisk: &compute.ManagedDiskParameters{
						ID:                 to.Ptr("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/disks/etcd-1.etcd-events.cluster"),
						StorageAccountType: to.Ptr(compute.StorageAccountTypes("PremiumV2_LRS")),
					},
				},
			},
			lun: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := newMockClient()
			client.vms[client.vmScaleSetInstanceID()] = &compute.VirtualMachineScaleSetVM{
				Properties: &compute.VirtualMachineScaleSetVMProperties{
					StorageProfile: &compute.StorageProfile{
						DataDisks: tc.dataDisks,
					},
				},
			}
			a, err := newAzureVolumes("cluster", []string{}, "nameTag", client)
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			// After the VM update, AttachVolume resolves the LUN through the host's /dev/disk/azure
			// symlinks, which do not exist where the tests run, so its error is ignored.
			_ = a.AttachVolume(&volumes.Volume{
				ProviderID: diskID,
				Info:       volumes.VolumeInfo{Description: diskName},
			})

			dataDisks := client.vms[client.vmScaleSetInstanceID()].Properties.StorageProfile.DataDisks
			if len(dataDisks) != len(tc.dataDisks)+1 {
				t.Fatalf("expected %d data disks, but got %d", len(tc.dataDisks)+1, len(dataDisks))
			}
			for i, d := range tc.dataDisks {
				if dataDisks[i] != d {
					t.Errorf("data disk %d was replaced", i)
				}
			}

			d := dataDisks[len(dataDisks)-1]
			if d.Lun == nil || *d.Lun != tc.lun {
				t.Errorf("expected lun %d, but got %v", tc.lun, d.Lun)
			}
			if d.Name == nil || *d.Name != diskName {
				t.Errorf("expected name %q, but got %v", diskName, d.Name)
			}
			if d.CreateOption == nil || *d.CreateOption != compute.DiskCreateOptionTypesAttach {
				t.Errorf("expected create option %q, but got %v", compute.DiskCreateOptionTypesAttach, d.CreateOption)
			}
			if d.Caching == nil || *d.Caching != compute.CachingTypesNone {
				t.Errorf("expected caching %q, but got %v", compute.CachingTypesNone, d.Caching)
			}
			if d.ManagedDisk == nil || d.ManagedDisk.ID == nil || *d.ManagedDisk.ID != diskID {
				t.Fatalf("expected managed disk %q, but got %+v", diskID, d.ManagedDisk)
			}
			if d.ManagedDisk.StorageAccountType != nil {
				t.Errorf("expected no storage account type, but got %q", *d.ManagedDisk.StorageAccountType)
			}
		})
	}
}
