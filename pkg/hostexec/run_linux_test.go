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
	"os"
	"path/filepath"
	"testing"
)

func TestFindInHostPaths(t *testing.T) {
	base := t.TempDir()

	if got, ok := findInHostPaths(base, "mount"); ok {
		t.Fatalf("expected mount to be absent, got %q", got)
	}

	if err := os.MkdirAll(filepath.Join(base, "bin"), 0755); err != nil {
		t.Fatalf("creating bin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "bin", "mount"), nil, 0755); err != nil {
		t.Fatalf("creating mount binary: %v", err)
	}

	if got, ok := findInHostPaths(base, "mount"); !ok || got != "/bin/mount" {
		t.Fatalf("expected /bin/mount, got %q (%t)", got, ok)
	}
}
