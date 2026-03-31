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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/linode/linodego"
	"k8s.io/klog/v2"
	"sigs.k8s.io/etcd-manager/pkg/volumes"
)

const (
	linodeTokenEnvVar = "LINODE_TOKEN"

	linodeMetadataBaseURL            = "http://169.254.169.254"
	linodeMetadataTokenPath          = "/v1/token"
	linodeMetadataInstancePath       = "/v1/instance"
	linodeMetadataTokenExpirySeconds = "300"
)

type linodeClient interface {
	ListVolumes(ctx context.Context, opts *linodego.ListOptions) ([]linodego.Volume, error)
	GetVolume(ctx context.Context, volumeID int) (*linodego.Volume, error)
	AttachVolume(ctx context.Context, volumeID int, opts *linodego.VolumeAttachOptions) (*linodego.Volume, error)
	GetInstance(ctx context.Context, linodeID int) (*linodego.Instance, error)
	GetInstanceIPAddresses(ctx context.Context, linodeID int) (*linodego.InstanceIPAddressResponse, error)
}

type metadataHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// LinodeVolumes defines the Linode (Akamai) volume implementation.
type LinodeVolumes struct {
	clusterName string

	matchPeerTags map[string]string
	matchNameTags map[string]string

	linodeClient linodeClient

	instanceID int
	region     string
}

var _ volumes.Volumes = &LinodeVolumes{}

// NewLinodeVolumes returns a new Linode (Akamai) volume provider.
func NewLinodeVolumes(clusterName string, volumeTags []string, nameTag string) (*LinodeVolumes, error) {
	token := strings.TrimSpace(os.Getenv(linodeTokenEnvVar))
	if token == "" {
		return nil, fmt.Errorf("%s is required", linodeTokenEnvVar)
	}

	instanceID, region, err := loadLinodeMetadata(context.TODO(), http.DefaultClient, linodeMetadataBaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to load linode metadata: %w", err)
	}

	linodeAPIClient := linodego.NewClient(nil)
	linodeAPIClient.SetUserAgent("etcd-manager")
	linodeAPIClient.SetToken(token)

	a := &LinodeVolumes{
		clusterName:   clusterName,
		matchPeerTags: make(map[string]string),
		matchNameTags: make(map[string]string),
		linodeClient:  &linodeAPIClient,
		instanceID:    instanceID,
		region:        region,
	}

	for _, volumeTag := range volumeTags {
		key, value, err := parseTagSpec(volumeTag)
		if err != nil {
			return nil, fmt.Errorf("parsing volume tag %q: %w", volumeTag, err)
		}
		a.matchPeerTags[key] = value
		a.matchNameTags[key] = value
	}

	if strings.TrimSpace(nameTag) != "" {
		key, value, err := parseTagSpec(nameTag)
		if err != nil {
			return nil, fmt.Errorf("parsing volume name tag %q: %w", nameTag, err)
		}
		a.matchNameTags[key] = value
	}

	return a, nil
}

// FindVolumes returns all volumes that can be attached to the running instance.
func (a *LinodeVolumes) FindVolumes() ([]*volumes.Volume, error) {
	return a.findMatchingVolumes(true, a.matchNameTags)
}

// FindMountedVolume returns the device where the volume is mounted to the running instance.
func (a *LinodeVolumes) FindMountedVolume(volume *volumes.Volume) (string, error) {
	device := volume.LocalDevice
	if device == "" {
		return "", fmt.Errorf("volume %q has an empty local device path", volume.ProviderID)
	}

	klog.V(2).Infof("Finding mounted volume %q", device)
	_, err := os.Stat(volumes.PathFor(device))
	if err == nil {
		klog.V(2).Infof("Found mounted volume %q", device)
		return device, nil
	}

	if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to find local device %q: %w", device, err)
	}

	// When not found, the interface says to return ("", nil)
	return "", nil
}

// AttachVolume attaches the specified volume to the running instance.
func (a *LinodeVolumes) AttachVolume(volume *volumes.Volume) error {
	volumeID, err := strconv.Atoi(volume.ProviderID)
	if err != nil {
		return fmt.Errorf("failed to convert volume id %q to int: %w", volume.ProviderID, err)
	}

	for {
		linodeVolume, err := a.linodeClient.GetVolume(context.TODO(), volumeID)
		if err != nil {
			return fmt.Errorf("failed to get info for volume id %q: %w", volume.ProviderID, err)
		}

		if linodeVolume.LinodeID != nil {
			if *linodeVolume.LinodeID != a.instanceID {
				return fmt.Errorf("found volume %s(%d) attached to a different instance: %d", linodeVolume.Label, linodeVolume.ID, *linodeVolume.LinodeID)
			}

			klog.V(2).Infof("Attached volume %s(%d) to the running instance", linodeVolume.Label, linodeVolume.ID)

			volume.LocalDevice = localDevicePath(linodeVolume)
			if volume.LocalDevice == "" {
				return fmt.Errorf("failed to determine local device path for volume %s(%d)", linodeVolume.Label, linodeVolume.ID)
			}
			return nil
		}

		klog.V(2).Infof("Attaching volume %s(%d) to the running instance", linodeVolume.Label, linodeVolume.ID)

		_, err = a.linodeClient.AttachVolume(context.TODO(), volumeID, &linodego.VolumeAttachOptions{LinodeID: a.instanceID})
		if err != nil {
			// This can race with other peers trying to claim a free volume.
			klog.V(2).Infof("Attach call for volume %s(%d) returned %v; will re-check state", linodeVolume.Label, linodeVolume.ID, err)
		}

		time.Sleep(5 * time.Second)
	}
}

// MyIP returns the first private IPv4 of the running instance if successful.
func (a *LinodeVolumes) MyIP() (string, error) {
	return a.getPrivateIPv4(context.TODO(), a.instanceID)
}

// findMatchingVolumes returns all volumes that match the specified tags and optionally filters by region.
func (a *LinodeVolumes) findMatchingVolumes(filterByRegion bool, matchTags map[string]string) ([]*volumes.Volume, error) {
	linodeVolumes, err := a.linodeClient.ListVolumes(context.TODO(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	var localEtcdVolumes []*volumes.Volume
	for i := range linodeVolumes {
		volume := &linodeVolumes[i]

		if filterByRegion && volume.Region != a.region {
			continue
		}

		if !tagsMatch(volume.Tags, matchTags) {
			continue
		}

		klog.V(2).Infof("Found attachable volume %s(%d) in region %q with status %q", volume.Label, volume.ID, volume.Region, volume.Status)

		localEtcdVolume := a.buildVolume(volume)
		localEtcdVolumes = append(localEtcdVolumes, localEtcdVolume)
	}

	return localEtcdVolumes, nil
}

// buildVolume converts a linodego.Volume into a local volumes.Volume representation.
func (a *LinodeVolumes) buildVolume(volume *linodego.Volume) *volumes.Volume {
	volumeID := strconv.Itoa(volume.ID)

	localEtcdVolume := &volumes.Volume{
		ProviderID: volumeID,
		Info: volumes.VolumeInfo{
			Description: a.clusterName + "-" + volumeID,
		},
		MountName: "linode-" + volumeID,
		EtcdName:  "vol-" + volumeID,
		Status:    string(volume.Status),
	}

	if volume.LinodeID != nil {
		localEtcdVolume.AttachedTo = strconv.Itoa(*volume.LinodeID)
		if *volume.LinodeID == a.instanceID {
			localEtcdVolume.LocalDevice = localDevicePath(volume)
		}
	}

	return localEtcdVolume
}

// localDevicePath returns the local device path for the specified Linode (Akamai) volume.
func localDevicePath(volume *linodego.Volume) string {
	if volume == nil {
		return ""
	}

	if volume.FilesystemPath != "" {
		return volume.FilesystemPath
	}

	label := sanitizeLabel(volume.Label)
	if label == "" {
		return ""
	}

	return "/dev/disk/by-id/linode-volume-" + label
}

// sanitizeLabel returns a sanitized version of the volume label suitable for use in a device path.
func sanitizeLabel(label string) string {
	replacer := strings.NewReplacer(" ", "-", "\t", "-", "/", "-")
	return strings.TrimSpace(replacer.Replace(label))
}

// getPrivateIPv4 returns the first private IPv4 address of the specified Linode (Akamai) instance if available.
func (a *LinodeVolumes) getPrivateIPv4(ctx context.Context, linodeID int) (string, error) {
	ipResponse, err := a.linodeClient.GetInstanceIPAddresses(ctx, linodeID)
	if err != nil {
		klog.V(4).Infof("failed to get instance %d IP address: %v", linodeID, err)
	} else if ipResponse != nil && ipResponse.IPv4 != nil {
		for _, ip := range ipResponse.IPv4.Private {
			if ip == nil || ip.Address == "" {
				continue
			}
			return ip.Address, nil
		}

		for _, ip := range ipResponse.IPv4.VPC {
			if ip == nil || ip.Address == nil || *ip.Address == "" {
				continue
			}
			return *ip.Address, nil
		}
	}

	instance, err := a.linodeClient.GetInstance(ctx, linodeID)
	if err != nil {
		return "", fmt.Errorf("failed to get instance %d: %w", linodeID, err)
	}

	for _, ip := range instance.IPv4 {
		if ip == nil {
			continue
		}

		if ip.To4() == nil || !ip.IsPrivate() {
			continue
		}

		return ip.String(), nil
	}

	return "", fmt.Errorf("failed to find private IPv4 for instance %d", linodeID)
}

// parseTagSpec parses a tag specification of the form "key=value" or "key:value" into its key and value components.
func parseTagSpec(tag string) (string, string, error) {
	key, value, _ := parseResourceTag(tag)
	if key == "" {
		return "", "", errors.New("tag key cannot be empty")
	}
	return key, value, nil
}

// parseResourceTag parses a resource tag of the form "key=value" or "key:value" into its key and value components.
func parseResourceTag(tag string) (key string, value string, hasValue bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", "", false
	}

	if strings.Contains(tag, "=") {
		tokens := strings.SplitN(tag, "=", 2)
		return strings.TrimSpace(tokens[0]), strings.TrimSpace(tokens[1]), true
	}

	if strings.Contains(tag, ":") {
		tokens := strings.SplitN(tag, ":", 2)
		return strings.TrimSpace(tokens[0]), strings.TrimSpace(tokens[1]), true
	}

	return tag, "", false
}

// tagsMatch returns true if all required tags are present in the resource tags.
func tagsMatch(resourceTags []string, requiredTags map[string]string) bool {
	for key, value := range requiredTags {
		if !hasTag(resourceTags, key, value) {
			return false
		}
	}

	return true
}

// hasTag returns true if the specified key and value are present in the resource tags.
func hasTag(resourceTags []string, requiredKey string, requiredValue string) bool {
	for _, tag := range resourceTags {
		key, value, hasValue := parseResourceTag(tag)
		if key != requiredKey {
			continue
		}

		if requiredValue == "" {
			return true
		}

		if hasValue && value == requiredValue {
			return true
		}
	}

	return false
}

// loadLinodeMetadata loads the Linode (Akamai) instance metadata including the instance ID and region.
func loadLinodeMetadata(ctx context.Context, client metadataHTTPClient, metadataBaseURL string) (int, string, error) {
	token, err := fetchLinodeMetadataToken(ctx, client, metadataBaseURL)
	if err != nil {
		return 0, "", err
	}

	instanceMetadata, err := fetchLinodeInstanceMetadata(ctx, client, metadataBaseURL, token)
	if err != nil {
		return 0, "", err
	}

	instanceIDString := parseLinodeMetadataValue(instanceMetadata, "id")
	if instanceIDString == "" {
		return 0, "", errors.New("instance id from metadata was empty")
	}

	instanceID, err := strconv.Atoi(instanceIDString)
	if err != nil {
		return 0, "", fmt.Errorf("invalid instance id %q: %w", instanceIDString, err)
	}

	region := parseLinodeMetadataValue(instanceMetadata, "region")
	if region == "" {
		return 0, "", errors.New("instance region from metadata was empty")
	}

	return instanceID, region, nil
}

// fetchLinodeMetadataToken fetches a metadata token from the Linode (Akamai) metadata service.
func fetchLinodeMetadataToken(ctx context.Context, client metadataHTTPClient, metadataBaseURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, metadataBaseURL+linodeMetadataTokenPath, nil)
	if err != nil {
		return "", fmt.Errorf("building metadata token request: %w", err)
	}
	req.Header.Set("Metadata-Token-Expiry-Seconds", linodeMetadataTokenExpirySeconds)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching metadata token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching metadata token: unexpected status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading metadata token response: %w", err)
	}

	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", errors.New("metadata token was empty")
	}

	return token, nil
}

// fetchLinodeInstanceMetadata fetches the instance metadata from the Linode (Akamai) metadata service using the provided token.
func fetchLinodeInstanceMetadata(ctx context.Context, client metadataHTTPClient, metadataBaseURL string, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataBaseURL+linodeMetadataInstancePath, nil)
	if err != nil {
		return "", fmt.Errorf("building instance metadata request: %w", err)
	}
	req.Header.Set("Metadata-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching instance metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching instance metadata: unexpected status code %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading instance metadata response: %w", err)
	}

	return string(body), nil
}

// parseLinodeMetadataValue parses the specified key from the Linode (Akamai) instance metadata and returns its value.
func parseLinodeMetadataValue(metadata string, key string) string {
	prefix := key + ":"
	for line := range strings.SplitSeq(metadata, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}

		return strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}

	return ""
}
