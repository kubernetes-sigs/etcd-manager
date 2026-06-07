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

package volumes

import (
	"strings"
	"testing"

	"k8s.io/mount-utils"
)

func TestContainerMountFSTypeUsesRequestedType(t *testing.T) {
	fakeMounter := mount.NewFakeMounter(nil)

	got, err := containerMountFSType(fakeMounter, "/mnt/etcd", "xfs")
	if err != nil {
		t.Fatalf("containerMountFSType: %v", err)
	}
	if got != "xfs" {
		t.Fatalf("expected xfs, got %q", got)
	}
}

func TestContainerMountFSTypeUsesHostMountType(t *testing.T) {
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{
		{Device: "/dev/sdb", Path: "/mnt/etcd", Type: "xfs"},
	})

	got, err := containerMountFSType(fakeMounter, "/mnt/etcd", "")
	if err != nil {
		t.Fatalf("containerMountFSType: %v", err)
	}
	if got != "xfs" {
		t.Fatalf("expected xfs, got %q", got)
	}
}

func TestContainerMountFSTypeRequiresHostMountType(t *testing.T) {
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{
		{Device: "/dev/sdb", Path: "/mnt/etcd"},
	})

	_, err := containerMountFSType(fakeMounter, "/mnt/etcd", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if got, want := err.Error(), "has no filesystem type"; !strings.Contains(got, want) {
		t.Fatalf("expected error to contain %q, got %q", want, got)
	}
}
