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

package hostexec

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

// Run executes the internal helper command.
func Run(args []string) int {
	fs := flag.NewFlagSet(HelperCommand, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootfs := fs.String("rootfs", "/rootfs", "host root filesystem mount")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cmdArgs := fs.Args()
	if len(cmdArgs) == 0 {
		fmt.Fprintf(os.Stderr, "%s: missing command\n", HelperCommand)
		return 2
	}

	if err := runHostCommand(filepath.Clean(*rootfs), cmdArgs[0], cmdArgs[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", HelperCommand, err)
		return 127
	}
	return 0
}

func runHostCommand(rootfs, cmd string, args []string) error {
	nsPath := filepath.Join(rootfs, "proc/1/ns/mnt")
	nsFD, err := unix.Open(nsPath, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("opening mount namespace %q: %w", nsPath, err)
	}
	defer unix.Close(nsFD)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := unix.Unshare(unix.CLONE_FS); err != nil {
		return fmt.Errorf("unsharing filesystem attributes: %w", err)
	}

	// Chroot first while /rootfs is visible from the container namespace. The
	// namespace fd stays open across chroot, so we can then join the host mount
	// namespace and exec host binaries with their host shared libraries.
	if err := unix.Chroot(rootfs); err != nil {
		return fmt.Errorf("chroot %q: %w", rootfs, err)
	}
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("chdir /: %w", err)
	}
	if err := unix.Setns(nsFD, unix.CLONE_NEWNS); err != nil {
		return fmt.Errorf("joining host mount namespace: %w", err)
	}

	if !filepath.IsAbs(cmd) {
		resolved, err := resolveCommand(cmd)
		if err != nil {
			return err
		}
		cmd = resolved
	}

	argv := append([]string{cmd}, args...)
	return unix.Exec(cmd, argv, os.Environ())
}

var hostSearchPaths = []string{"/", "/bin", "/usr/sbin", "/usr/bin", "/sbin"}

func resolveCommand(cmd string) (string, error) {
	if p, ok := findInHostPaths("/", cmd); ok {
		return p, nil
	}
	return "", fmt.Errorf("unable to find %q in host PATH", cmd)
}

// findInHostPaths looks for command in the well-known host bin directories,
// statting each candidate under base. It returns the path relative to the host
// root (i.e. without the base prefix).
func findInHostPaths(base, command string) (string, bool) {
	for _, dir := range hostSearchPaths {
		p := filepath.Join(dir, command)
		if _, err := os.Stat(filepath.Join(base, p)); err == nil {
			return p, true
		}
	}
	return "", false
}
