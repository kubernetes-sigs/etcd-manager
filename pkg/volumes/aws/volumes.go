/*
Copyright 2016 The Kubernetes Authors.

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

package aws

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"k8s.io/klog/v2"
	"k8s.io/kops/util/pkg/awslog"

	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

var devices = []string{"/dev/xvdu", "/dev/xvdv", "/dev/xvdx", "/dev/xvdx", "/dev/xvdy", "/dev/xvdz"}

// AWSVolumes defines the aws volume implementation
type AWSVolumes struct {
	mutex sync.Mutex

	matchTagKeys []string
	matchTags    map[string]string
	nameTag      string
	clusterName  string

	deviceMap  map[string]string
	ec2        *ec2.Client
	instanceID string
	localIP    string
	imds       *imds.Client
	zone       string
}

var _ volumes.Volumes = &AWSVolumes{}

// NewAWSVolumes returns a new aws volume provider
func NewAWSVolumes(clusterName string, volumeTags []string, nameTag string) (*AWSVolumes, error) {
	ctx := context.TODO()
	a := &AWSVolumes{
		clusterName: clusterName,
		deviceMap:   make(map[string]string),
		matchTags:   make(map[string]string),
		nameTag:     nameTag,
	}

	for _, volumeTag := range volumeTags {
		tokens := strings.SplitN(volumeTag, "=", 2)
		if len(tokens) == 1 {
			a.matchTagKeys = append(a.matchTagKeys, tokens[0])
		} else {
			a.matchTags[tokens[0]] = tokens[1]
		}
	}

	config, err := awsconfig.LoadDefaultConfig(ctx, awslog.WithAWSLogger())
	if err != nil {
		return nil, fmt.Errorf("error loading AWS config: %v", err)
	}

	a.imds = imds.NewFromConfig(config)

	regionResp, err := a.imds.GetRegion(ctx, &imds.GetRegionInput{})
	if err != nil {
		return nil, fmt.Errorf("error querying ec2 metadata service (for az/region): %v", err)
	}
	region := regionResp.Region

	zoneResp, err := a.imds.GetMetadata(ctx, &imds.GetMetadataInput{Path: "placement/availability-zone"})
	if err != nil {
		return nil, fmt.Errorf("error querying ec2 metadata service (for az): %v", err)
	}
	zone, err := io.ReadAll(zoneResp.Content)
	if err != nil {
		return nil, fmt.Errorf("error reading ec2 metadata service response (for az): %v", err)
	}
	a.zone = string(zone)

	instanceIDResp, err := a.imds.GetMetadata(ctx, &imds.GetMetadataInput{Path: "instance-id"})
	if err != nil {
		return nil, fmt.Errorf("error querying ec2 metadata service (for instance-id): %v", err)
	}
	instanceID, err := io.ReadAll(instanceIDResp.Content)
	if err != nil {
		return nil, fmt.Errorf("error reading ec2 metadata service response (for instance-id): %v", err)
	}
	a.instanceID = string(instanceID)

	a.ec2 = ec2.NewFromConfig(config, func(o *ec2.Options) {
		o.Region = region
	})
	ipv6Resp, err := a.imds.GetMetadata(ctx, &imds.GetMetadataInput{Path: "ipv6"})
	// If we have an IPv6 address, return
	if err == nil {
		ipv6, err := io.ReadAll(ipv6Resp.Content)
		if err != nil {
			return nil, fmt.Errorf("error reading ec2 metadata service response (for ipv6): %v", err)
		}
		a.localIP = string(ipv6)

		return a, nil
	}
	var awsErr *awshttp.ResponseError
	if errors.As(err, &awsErr) && awsErr.HTTPStatusCode() != http.StatusNotFound {
		return nil, fmt.Errorf("error querying ec2 metadata service (ipv6): %v", err)
	} else {
		ipv4Resp, err := a.imds.GetMetadata(ctx, &imds.GetMetadataInput{Path: "local-ipv4"})
		if err != nil {
			return nil, fmt.Errorf("error querying ec2 metadata service (for local-ipv4): %v", err)
		}
		ipv4, err := io.ReadAll(ipv4Resp.Content)
		if err != nil {
			return nil, fmt.Errorf("error reading ec2 metadata service response (for local-ipv4): %v", err)
		}
		a.localIP = string(ipv4)
	}

	return a, nil
}

func (a *AWSVolumes) describeInstance() (*ec2types.Instance, error) {
	request := &ec2.DescribeInstancesInput{}
	request.InstanceIds = []string{a.instanceID}

	var instances []ec2types.Instance
	paginator := ec2.NewDescribeInstancesPaginator(a.ec2, request)
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(context.Background())
		if err != nil {
			return nil, fmt.Errorf("error querying for EC2 instance %q: %v", a.instanceID, err)
		}
		for _, r := range output.Reservations {
			instances = append(instances, r.Instances...)
		}
	}

	if len(instances) != 1 {
		return nil, fmt.Errorf("unexpected number of instances found with id %q: %d", a.instanceID, len(instances))
	}

	return &instances[0], nil
}

func newEc2Filter(name string, value string) ec2types.Filter {
	filter := ec2types.Filter{
		Name: aws.String(name),
		Values: []string{
			value,
		},
	}
	return filter
}

func (a *AWSVolumes) describeVolumes(ctx context.Context, request *ec2.DescribeVolumesInput) ([]*volumes.Volume, error) {
	var found []*volumes.Volume
	paginator := ec2.NewDescribeVolumesPaginator(a.ec2, request)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("error querying for EC2 volumes: %v", err)
		}

		for _, v := range output.Volumes {
			etcdName := aws.ToString(v.VolumeId)
			if a.nameTag != "" {
				for _, t := range v.Tags {
					if a.nameTag == aws.ToString(t.Key) {
						v := aws.ToString(t.Value)
						if v != "" {
							tokens := strings.SplitN(v, "/", 2)
							etcdName = a.clusterName + "-" + tokens[0]
						}
					}
				}
			}

			vol := &volumes.Volume{
				MountName:  "master-" + aws.ToString(v.VolumeId),
				ProviderID: aws.ToString(v.VolumeId),
				EtcdName:   etcdName,
				Info: volumes.VolumeInfo{
					Description: aws.ToString(v.VolumeId),
				},
			}

			vol.Status = string(v.State)

			for _, attachment := range v.Attachments {
				vol.AttachedTo = aws.ToString(attachment.InstanceId)
				if aws.ToString(attachment.InstanceId) == a.instanceID {
					vol.LocalDevice = aws.ToString(attachment.Device)
				}
			}

			// never mount root volumes
			// these are volumes that aws sets aside for root volumes mount points
			if vol.LocalDevice == "/dev/sda1" || vol.LocalDevice == "/dev/xvda" {
				klog.Warningf("Not mounting: %q, since it is a root volume", vol.LocalDevice)
				continue
			}

			found = append(found, vol)
		}
	}
	return found, nil
}

func (a *AWSVolumes) FindVolumes() ([]*volumes.Volume, error) {
	return a.findVolumes(context.TODO(), true)
}

func (a *AWSVolumes) findVolumes(ctx context.Context, filterByAZ bool) ([]*volumes.Volume, error) {
	request := &ec2.DescribeVolumesInput{}

	if filterByAZ {
		request.Filters = append(request.Filters, newEc2Filter("availability-zone", a.zone))
	}

	for k, v := range a.matchTags {
		request.Filters = append(request.Filters, newEc2Filter("tag:"+k, v))
	}
	for _, k := range a.matchTagKeys {
		request.Filters = append(request.Filters, newEc2Filter("tag-key", k))
	}

	return a.describeVolumes(ctx, request)
}

// FindMountedVolume implements Volumes::FindMountedVolume
func (a *AWSVolumes) FindMountedVolume(volume *volumes.Volume) (string, error) {
	device := volume.LocalDevice

	_, err := os.Stat(volumes.PathFor(device))
	if err == nil {
		return device, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("error checking for device %q: %v", device, err)
	}
	klog.V(2).Infof("volume %s not mounted at %s", volume.ProviderID, volumes.PathFor(device))

	if volume.ProviderID != "" {
		expected := volume.ProviderID
		expected = "nvme-Amazon_Elastic_Block_Store_" + strings.Replace(expected, "-", "", -1)

		// Look for nvme devices
		// On AWS, nvme volumes are not mounted on a device path, but are instead mounted on an nvme device
		// We must identify the correct volume by matching the nvme info
		device, err := findNvmeVolume(expected)
		if err != nil {
			return "", fmt.Errorf("error checking for nvme volume %q: %v", expected, err)
		}
		if device != "" {
			klog.Infof("found nvme volume %q at %q", expected, device)
			return device, nil
		}
		klog.V(2).Infof("volume %s not mounted at %s", volume.ProviderID, expected)
	}

	// When not found, the interface says we return ("", nil)
	return "", nil
}

func findNvmeVolume(findName string) (device string, err error) {
	p := volumes.PathFor(filepath.Join("/dev/disk/by-id", findName))
	stat, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			klog.V(4).Infof("nvme path not found %q", p)
			return "", nil
		}
		return "", fmt.Errorf("error getting stat of %q: %v", p, err)
	}

	if stat.Mode()&os.ModeSymlink != os.ModeSymlink {
		klog.Warningf("nvme file %q found, but was not a symlink", p)
		return "", nil
	}

	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("error reading target of symlink %q: %v", p, err)
	}

	// Reverse pathFor
	devPath := volumes.PathFor("/dev")
	if strings.HasPrefix(resolved, devPath) {
		resolved = strings.Replace(resolved, devPath, "/dev", 1)
	}

	if !strings.HasPrefix(resolved, "/dev") {
		return "", fmt.Errorf("resolved symlink for %q was unexpected: %q", p, resolved)
	}

	return resolved, nil
}

// assignDevice picks a hopefully unused device and reserves it for the volume attachment
func (a *AWSVolumes) assignDevice(volumeID string) (string, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	// TODO: Check for actual devices in use (like cloudprovider does)
	for _, d := range devices {
		if a.deviceMap[d] == "" {
			a.deviceMap[d] = volumeID
			return d, nil
		}
	}
	return "", fmt.Errorf("all devices in use")
}

// releaseDevice releases the volume mapping lock; used when an attach was known to fail
func (a *AWSVolumes) releaseDevice(d string, volumeID string) {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	if a.deviceMap[d] != volumeID {
		klog.Fatalf("deviceMap logic error: %q -> %q, not %q", d, a.deviceMap[d], volumeID)
	}
	a.deviceMap[d] = ""
}

// AttachVolume attaches the specified volume to this instance, returning the mountpoint & nil if successful
func (a *AWSVolumes) AttachVolume(volume *volumes.Volume) error {
	ctx := context.TODO()
	volumeID := volume.ProviderID

	device := volume.LocalDevice
	if device == "" {
		for {
			d, err := a.assignDevice(volumeID)
			if err != nil {
				return err
			}
			device = d

			klog.V(2).Infof("Trying to attach volume %q at %q", volumeID, device)

			request := &ec2.AttachVolumeInput{
				Device:     aws.String(device),
				InstanceId: aws.String(a.instanceID),
				VolumeId:   aws.String(volumeID),
			}

			attachResponse, err := a.ec2.AttachVolume(ctx, request)
			if err != nil {
				var apiErr smithy.APIError
				if errors.As(err, &apiErr) && apiErr.ErrorCode() == "InvalidParameterValue" {
					klog.Warning(apiErr.ErrorMessage())
					continue
				}
				return fmt.Errorf("error attaching EBS volume %q: %v", volumeID, err)
			}

			klog.V(2).Infof("AttachVolume request returned %v", attachResponse)
			break
		}
	}

	// Wait (forever) for volume to attach or reach a failure-to-attach condition
	for {
		request := &ec2.DescribeVolumesInput{
			VolumeIds: []string{volumeID},
		}

		volumes, err := a.describeVolumes(ctx, request)
		if err != nil {
			return fmt.Errorf("error describing EBS volume %q: %v", volumeID, err)
		}

		if len(volumes) == 0 {
			return fmt.Errorf("EBS volume %q disappeared during attach", volumeID)
		}
		if len(volumes) != 1 {
			return fmt.Errorf("multiple volumes found with id %q", volumeID)
		}

		v := volumes[0]
		if v.AttachedTo != "" {
			if v.AttachedTo == a.instanceID {
				// TODO: Wait for device to appear?

				volume.LocalDevice = device
				return nil
			} else {
				a.releaseDevice(device, volumeID)

				return fmt.Errorf("unable to attach volume %q, was attached to %q", volumeID, v.AttachedTo)
			}
		}

		switch v.Status {
		case "attaching":
			klog.V(2).Infof("Waiting for volume %q to be attached (currently %q)", volumeID, v.Status)
			// continue looping

		default:
			return fmt.Errorf("observed unexpected volume state %q", v.Status)
		}

		time.Sleep(10 * time.Second)
	}
}

func (a *AWSVolumes) MyIP() (string, error) {
	return a.localIP, nil
}
