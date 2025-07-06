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
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	compute "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

func TestFindVolumes(t *testing.T) {
	client := newMockClient()
	a, err := newAzureVolumes("cluster", []string{}, "nameTag", client)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	client.disks = []*compute.Disk{
		{
			Name: to.Ptr("name0"),
			ID:   to.Ptr("id0"),
			Properties: &compute.DiskProperties{
				DiskState: to.Ptr(compute.DiskState("state")),
			},
			ManagedBy: to.Ptr(client.name()),
		},
		{
			// Attached to an other VM.
			Name: to.Ptr("name1"),
			ID:   to.Ptr("id1"),
			Properties: &compute.DiskProperties{
				DiskState: to.Ptr(compute.DiskState("state")),
			},
			ManagedBy: to.Ptr("other"),
		},
		{
			// Unmanaged disk.
			Name: to.Ptr("name2"),
			ID:   to.Ptr("id2"),
			Properties: &compute.DiskProperties{
				DiskState: to.Ptr(compute.DiskState("state")),
			},
		},
	}

	client.dDisks = []*dataDisk{
		{
			Lun: "0",
			ManagedDisk: &managedDisk{
				ID: "id0",
			},
		},
	}

	actual, err := a.FindVolumes()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	expected := []*volumes.Volume{
		{
			MountName:  "master-name0",
			ProviderID: "id0",
			EtcdName:   "name0",
			Status:     "state",
			Info: volumes.VolumeInfo{
				Description: "name0",
			},
			AttachedTo:  client.name(),
			LocalDevice: "/dev/sdc",
		},
		{
			MountName:  "master-name1",
			ProviderID: "id1",
			EtcdName:   "name1",
			Status:     "state",
			Info: volumes.VolumeInfo{
				Description: "name1",
			},
			AttachedTo: "other",
		},
		{
			MountName:  "master-name2",
			ProviderID: "id2",
			EtcdName:   "name2",
			Status:     "state",
			Info: volumes.VolumeInfo{
				Description: "name2",
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected %+v, but got +%v", expected, actual)
	}
}

func TestAttachVolume(t *testing.T) {
	client := newMockClient()
	a, err := newAzureVolumes("cluster", []string{}, "nameTag", client)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	volume := &volumes.Volume{
		ProviderID: "id",
		Info: volumes.VolumeInfo{
			Description: "name",
		},
	}

	client.vms["0"] = &compute.VirtualMachineScaleSetVM{
		Properties: &compute.VirtualMachineScaleSetVMProperties{
			StorageProfile: &compute.StorageProfile{
				DataDisks: []*compute.DataDisk{},
			},
		},
	}

	if err := a.AttachVolume(volume); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	actual := client.vms["0"].Properties.StorageProfile.DataDisks
	expected := []*compute.DataDisk{
		{
			Lun:          to.Ptr(int32(0)),
			Name:         to.Ptr("name"),
			CreateOption: to.Ptr(compute.DiskCreateOptionTypesAttach),
			ManagedDisk: &compute.ManagedDiskParameters{
				StorageAccountType: to.Ptr(compute.StorageAccountTypesStandardSSDLRS),
				ID:                 to.Ptr("id"),
			},
		},
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("expected %+v, but got +%v", expected, actual)
	}
}

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
