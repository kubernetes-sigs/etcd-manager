//go:build linux
// +build linux

/*
Copyright 2019 The Kubernetes Authors.

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
	"path/filepath"

	"k8s.io/klog/v2"
	"k8s.io/mount-utils"
	"sigs.k8s.io/etcd-manager/pkg/hostexec"
)

// Based on code from kubernetes/kubernetes: https://github.com/kubernetes/kubernetes/blob/release-1.15/pkg/volume/util/nsenter/nsenter_mount.go

func New(rootfs string, exec *hostexec.Executor) *Mounter {
	return &Mounter{rootfs: filepath.Clean(rootfs), exec: exec}
}

type Mounter struct {
	rootfs string
	exec   *hostexec.Executor
	mount.Interface
}

var _ mount.Interface = &Mounter{}

// List returns a list of all mounted filesystems in the host's mount namespace.
func (n *Mounter) List() ([]mount.MountPoint, error) {
	return mount.ListProcMounts(filepath.Join(n.rootfs, "proc/1/mounts"))
}

// Mount runs mount(8) in the host's root mount namespace.  Aside from this
// aspect, Mount has the same semantics as the mounter returned by mount.New()
func (n *Mounter) Mount(source string, target string, fstype string, options []string) error {
	return n.MountSensitive(source, target, fstype, options, nil)
}

// MountSensitive is the same as Mount, except sensitiveOptions are not
// supported by this implementation.
func (n *Mounter) MountSensitive(source string, target string, fstype string, options []string, sensitiveOptions []string) error {
	if len(sensitiveOptions) != 0 {
		return fmt.Errorf("sensitiveOptions not supported by implementation of MountSensitive")
	}

	bind, bindOpts, bindRemountOpts := mount.MakeBindOpts(options)
	if bind {
		err := n.doHostMount(source, target, fstype, bindOpts)
		if err != nil {
			return err
		}
		return n.doHostMount(source, target, fstype, bindRemountOpts)
	}

	return n.doHostMount(source, target, fstype, options)
}

// doHostMount runs the requested mount in the host's mount namespace.
//
// The "mount" binary is resolved against the host PATH by the hostexec helper
// after it chroots into the host root. Unlike the upstream nsenter mounter we
// don't wrap mount in a systemd-run scope: that exists to keep fuse daemons
// alive across kubelet restarts, but etcd-manager only ever mounts plain block
// devices, so there is no daemon to preserve.
func (n *Mounter) doHostMount(source, target, fstype string, options []string) error {
	klog.V(5).Infof("host mount %s %s %s %v", source, target, fstype, options)
	mountArgs := mount.MakeMountArgs(source, target, fstype, options)
	outputBytes, err := n.exec.Command("mount", mountArgs...).CombinedOutput()
	if len(outputBytes) != 0 {
		klog.V(5).Infof("Output of mounting %s to %s: %v", source, target, string(outputBytes))
	}
	return err
}

// We deliberately implement only the functions we need, so we don't have to maintain them...

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
