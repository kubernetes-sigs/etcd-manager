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
	"reflect"
	"testing"
)

func TestExecutorLeavesDirectCommandLookupToHelper(t *testing.T) {
	rootfs := t.TempDir()
	e := &Executor{rootfs: rootfs, self: "/etcd-manager"}

	got := e.helperArgs("mount")
	want := []string{HelperCommand, "--rootfs", rootfs, "--", "mount"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}
