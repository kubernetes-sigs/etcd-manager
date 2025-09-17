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
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	compute "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute"
	network "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork"
	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

type clientInterface interface {
	name() string
	vmScaleSetInstanceID() string
	dataDisks() []*dataDisk
	refreshMetadata() error
	localIP() net.IP

	listVMScaleSetVMs(ctx context.Context) ([]*compute.VirtualMachineScaleSetVM, error)
	getVMScaleSetVM(ctx context.Context, instanceID string) (*compute.VirtualMachineScaleSetVM, error)
	updateVMScaleSetVM(ctx context.Context, instanceID string, parameters compute.VirtualMachineScaleSetVM) error
	listDisks(ctx context.Context) ([]*compute.Disk, error)
	listVMSSNetworkInterfaces(ctx context.Context) ([]*network.Interface, error)
}

var _ clientInterface = &client{}

// AzureVolumes defines the aws volume implementation
type AzureVolumes struct {
	clusterName string
	// matchTagKeys is a map of tag keys and tag values. Managed
	// disks that have the matching keys/values are managed by
	// this instance.
	matchTagKeys map[string]struct{}
	// matchTags is a map of tag keys. Managed disks that have the
	// matching tag keys are managed by this instance.
	matchTags map[string]string
	// nameTag is the name of the Managed Disk tag used to extract
	// an etcd name.
	nameTag string

	// instanceID is the name of this instance.
	instanceID string
	// localIP is the IP of this instance.
	localIP net.IP
	client  clientInterface
}

var _ volumes.Volumes = &AzureVolumes{}

// NewAzureVolumes returns a new Azure volume provider.
func NewAzureVolumes(clusterName string, volumeTags []string, nameTag string) (*AzureVolumes, error) {
	client, err := newClient()
	if err != nil {
		return nil, fmt.Errorf("error creating a new Azure client: %w", err)
	}
	return newAzureVolumes(clusterName, volumeTags, nameTag, client)
}

func newAzureVolumes(clusterName string, volumeTags []string, nameTag string, client clientInterface) (*AzureVolumes, error) {
	// Create matchTagKeys and matchTags from volumeTags.
	matchTagKeys := map[string]struct{}{}
	matchTags := map[string]string{}
	for _, volumeTag := range volumeTags {
		l := strings.SplitN(volumeTag, "=", 2)
		if len(l) == 1 {
			matchTagKeys[l[0]] = struct{}{}
		} else {
			matchTags[l[0]] = l[1]
		}
	}

	instanceID := client.name()
	if instanceID == "" {
		return nil, fmt.Errorf("empty name")
	}
	localIP := client.localIP()
	if localIP == nil {
		return nil, fmt.Errorf("error querying internal IP")
	}
	return &AzureVolumes{
		clusterName:  clusterName,
		matchTagKeys: matchTagKeys,
		matchTags:    matchTags,
		nameTag:      nameTag,
		instanceID:   instanceID,
		localIP:      localIP,
		client:       client,
	}, nil
}

// MyIP returns the IP of the instance.
func (a *AzureVolumes) MyIP() (string, error) {
	return a.localIP.String(), nil
}

// FindVolumes returns a list of volumes for this etcd cluster.
func (a *AzureVolumes) FindVolumes() ([]*volumes.Volume, error) {
	disks, err := a.client.listDisks(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("error listing disks: %w", err)
	}

	// Query instance metadata again before checking the status of attached data disks.
	if err := a.client.refreshMetadata(); err != nil {
		return nil, fmt.Errorf("error refreshling metadata: %w", err)
	}

	var vs []*volumes.Volume
	for _, disk := range disks {
		if !a.isDiskForCluster(disk) {
			continue
		}

		v := &volumes.Volume{
			MountName:  "master-" + *disk.Name,
			ProviderID: *disk.ID,
			EtcdName:   a.extractEtcdName(disk),
			Status:     string(*disk.Properties.DiskState),
			Info: volumes.VolumeInfo{
				Description: *disk.Name,
			},
		}

		if disk.ManagedBy != nil {
			// Extract the VMSS instance name.
			l := strings.Split(*disk.ManagedBy, "/")
			v.AttachedTo = l[len(l)-1]
			if v.AttachedTo == a.instanceID {
				ld, err := a.findLocalDevice(disk)
				if err != nil {
					return nil, fmt.Errorf("error finding a local device: %w", err)
				}
				v.LocalDevice = ld
			}
		}

		vs = append(vs, v)
	}
	return vs, nil
}

// findLocalDevice returns  the name of the local device of the given Managed Disk.
func (a *AzureVolumes) findLocalDevice(disk *compute.Disk) (string, error) {
	// Find a corresponding data disk.
	var found *dataDisk
	for _, dataDisk := range a.client.dataDisks() {
		if *disk.ID == dataDisk.ManagedDisk.ID {
			found = dataDisk
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("no data disk found for %s", *disk.ID)
	}

	dev, err := lunToDev(strings.TrimSpace(found.Lun))
	if err != nil {
		return "", err
	}
	return dev, nil
}

// FindMountedVolume returns the device name of the mounted volume.
func (a *AzureVolumes) FindMountedVolume(volume *volumes.Volume) (string, error) {
	dev := volume.LocalDevice

	_, err := os.Stat(volumes.PathFor(dev))
	if err == nil {
		return dev, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("error checking for device %q: %v", dev, err)
	}
	klog.V(2).Infof("volume %s not mounted at %s", volume.ProviderID, volumes.PathFor(dev))

	// TODO(kenji): Do more check.

	return dev, nil
}

// AttachVolume attaches the specified volume to this instance, returning nil if successful.
func (a *AzureVolumes) AttachVolume(volume *volumes.Volume) error {
	if volume.LocalDevice == "" {
		// The volume has not yet been attached. Attach ctx.

		ctx := context.TODO()
		instID := a.client.vmScaleSetInstanceID()
		vm, err := a.client.getVMScaleSetVM(ctx, instID)
		if err != nil {
			return fmt.Errorf("error getting VM: %w", err)
		}

		// Update the VM and append a new data disk.
		lun, err := a.findAvailableLun()
		if err != nil {
			return fmt.Errorf("error finding available Lun: %w", err)
		}

		d := &compute.DataDisk{
			Lun: to.Ptr(lun),
			// This is a hack to get a disk name from the description.
			Name:         to.Ptr(volume.Info.Description),
			CreateOption: to.Ptr(compute.DiskCreateOptionTypesAttach),
			ManagedDisk: &compute.ManagedDiskParameters{
				StorageAccountType: to.Ptr(compute.StorageAccountTypesStandardSSDLRS),
				ID:                 to.Ptr(volume.ProviderID),
			},
		}
		dds := append(vm.Properties.StorageProfile.DataDisks, d)
		vm.Properties.StorageProfile.DataDisks = dds
		if err := a.client.updateVMScaleSetVM(ctx, instID, *vm); err != nil {
			return fmt.Errorf("error updating VM: %w", err)
		}

		localDevice, err := lunToDev(strconv.Itoa(int(lun)))
		if err != nil {
			return err
		}
		volume.LocalDevice = localDevice
	}

	// TODO(kenji): Wait until the disk is attached to the instance.

	return nil
}

// isDiskForCluster returns true if the managed disk is for the cluster.
// TODO(kenji): Filter by availability zone?
func (a *AzureVolumes) isDiskForCluster(disk *compute.Disk) bool {
	found := 0
	for k, v := range disk.Tags {
		if _, ok := a.matchTagKeys[k]; ok {
			found++
		}
		if a.matchTags[k] == *v {
			found++
		}
	}
	return found == len(a.matchTagKeys)+len(a.matchTags)
}

// extractEtcdName extracts the Etcd name from the tag of the disk.
func (a *AzureVolumes) extractEtcdName(disk *compute.Disk) string {
	if a.nameTag == "" {
		return *disk.Name
	}
	v, ok := disk.Tags[a.nameTag]
	if !ok || *v == "" {
		return *disk.Name
	}
	l := strings.SplitN(*v, "/", 2)
	return a.clusterName + "-" + l[0]
}

// findAvailableLun returns a next available Lun.
// This function scans all data disks attached to the instance
// and returns the next Lun.
//
// When there are multiple concurrent attempts to find an available
// Lun, they can pick up the same Lun. When that happens, only one
// request for updating the VM and attching the disk will succeed (and
// then the rest of requests will be retried).
func (a *AzureVolumes) findAvailableLun() (int32, error) {
	ctx := context.TODO()
	instID := a.client.vmScaleSetInstanceID()
	vm, err := a.client.getVMScaleSetVM(ctx, instID)
	if err != nil {
		return 0, fmt.Errorf("error getting VM: %w", err)
	}

	if len(vm.Properties.StorageProfile.DataDisks) == 0 {
		return 0, nil
	}

	var maxLun int32
	for _, dd := range vm.Properties.StorageProfile.DataDisks {
		if lun := *dd.Lun; lun > maxLun {
			maxLun = lun
		}
	}
	return maxLun + 1, nil
}

// lunToDev returns a device for a given Lun.
func lunToDev(lunStr string) (string, error) {
	// Azure’s stable device-to-LUN mapping for Linux data disks.
	lunDevicesPath := volumes.PathFor("/dev/disk/azure/data/by-lun")
	if stat, err := os.Stat(lunDevicesPath); err == nil && stat.IsDir() {
		lunPath := filepath.Join(lunDevicesPath, lunStr)
		lunDevice, err := os.Readlink(lunPath)
		if err != nil {
			klog.Warningf("error reading the symbolic link %q: %v", lunPath, err)
		} else {
			deviceName := filepath.Base(lunDevice)
			return filepath.Join("/dev", deviceName), nil
		}
	}

	// Azure’s stable device-to-LUN mapping for Linux SCSI data disks.
	scsiDevicesPath := volumes.PathFor("/dev/disk/azure/scsi1")
	if stat, err := os.Stat(scsiDevicesPath); err == nil && stat.IsDir() {
		lunPath := filepath.Join(scsiDevicesPath, "lun"+lunStr)
		lunDevice, err := os.Readlink(lunPath)
		if err != nil {
			klog.Warningf("error reading the symbolic link %q: %v", lunPath, err)
		} else {
			deviceName := filepath.Base(lunDevice)
			return filepath.Join("/dev", deviceName), nil
		}
	}

	return "", fmt.Errorf("error finding the local device mapping for LUN %q", lunStr)
}
