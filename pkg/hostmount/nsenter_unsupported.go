//go:build !linux
// +build !linux

/*
Copyright 2017 The Kubernetes Authors.

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

package hostmount

import (
	"fmt"
	"k8s.io/mount-utils"
	"k8s.io/utils/nsenter"
)

func New(ne *nsenter.Nsenter) *Mounter {
	return &Mounter{ne: ne}
}

type Mounter struct {
	ne *nsenter.Nsenter
	mount.Interface
}

var _ mount.Interface = &Mounter{}

func (*Mounter) List() ([]mount.MountPoint, error) {
	return nil, fmt.Errorf("List not implemented for containerized mounter")
}

func (n *Mounter) Mount(source string, target string, fstype string, options []string) error {
	return fmt.Errorf("Mount not implemented for containerized mounter")
}

func (n *Mounter) MountSensitive(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	return fmt.Errorf("MountSensitive not implemented for containerized mounter")
}

func (n *Mounter) GetMountRefs(pathname string) ([]string, error) {
	return nil, fmt.Errorf("GetMountRefs not implemented for containerized mounter")
}

func (mounter *Mounter) IsLikelyNotMountPoint(file string) (bool, error) {
	return false, fmt.Errorf("IsLikelyNotMountPoint not implemented for containerized mounter")
}

func (n *Mounter) Unmount(target string) error {
	return fmt.Errorf("Unmount not implemented for containerized mounter")
}

func (n *Mounter) MountSensitiveWithoutSystemd(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	return fmt.Errorf("MountSensitiveWithoutSystemd not implemented for containerized mounter")
}

func (n *Mounter) MountSensitiveWithoutSystemdWithMountFlags(source string, target string, fstype string, options []string, sensitiveOptions []string, mountFlags []string) error {
	return fmt.Errorf("MountSensitiveWithoutSystemdWithMountFlags not implemented for containerized mounter")
}
