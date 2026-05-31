//go:build linux
// +build linux

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

package sysmount

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// Mount mounts source on target.
func Mount(source, target, fstype string, options []string) error {
	flags, err := mountOptions(options)
	if err != nil {
		return err
	}
	if err := unix.Mount(source, target, fstype, flags, ""); err != nil {
		return fmt.Errorf("mount %q on %q type %q options %v: %w", source, target, fstype, options, err)
	}
	return nil
}

// Unmount unmounts target.
func Unmount(target string) error {
	if err := unix.Unmount(target, 0); err != nil {
		return fmt.Errorf("unmount %q: %w", target, err)
	}
	return nil
}

func mountOptions(options []string) (uintptr, error) {
	var flags uintptr
	for _, option := range options {
		switch option {
		case "", "defaults", "rw":
		case "ro":
			flags |= unix.MS_RDONLY
		default:
			return 0, fmt.Errorf("syscall mounter does not support mount option %q", option)
		}
	}
	return flags, nil
}
