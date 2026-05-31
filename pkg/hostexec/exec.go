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
	"context"
	"fmt"
	"os"
	"path/filepath"

	utilexec "k8s.io/utils/exec"
)

const (
	// HelperCommand is the internal argv[1] used when etcd-manager re-execs itself
	// to run a command against the mounted host root.
	HelperCommand = "__hostexec"
)

// Executor runs host commands by re-execing etcd-manager in a small helper mode.
type Executor struct {
	rootfs   string
	self     string
	executor utilexec.Interface
}

// New returns an executor that runs commands in the host mount namespace and
// chrooted to rootfs.
func New(rootfs string) (*Executor, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding etcd-manager executable: %w", err)
	}

	return &Executor{
		rootfs:   filepath.Clean(rootfs),
		self:     self,
		executor: utilexec.New(),
	}, nil
}

// Command implements exec.Interface.
func (e *Executor) Command(cmd string, args ...string) utilexec.Cmd {
	return e.executor.Command(e.self, e.helperArgs(cmd, args...)...)
}

// CommandContext implements exec.Interface.
func (e *Executor) CommandContext(ctx context.Context, cmd string, args ...string) utilexec.Cmd {
	return e.executor.CommandContext(ctx, e.self, e.helperArgs(cmd, args...)...)
}

// LookPath implements exec.Interface. Command resolution happens in the helper
// after it chroots into the host root, so there is nothing to resolve here.
func (e *Executor) LookPath(file string) (string, error) {
	return "", utilexec.ErrExecutableNotFound
}

func (e *Executor) helperArgs(cmd string, args ...string) []string {
	helperArgs := []string{HelperCommand, "--rootfs", e.rootfs, "--", cmd}
	return append(helperArgs, args...)
}
