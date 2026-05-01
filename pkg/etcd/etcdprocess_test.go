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

package etcd

import "testing"

func TestSnapshotRestoreCommand(t *testing.T) {
	grid := []struct {
		etcdVersion string
		expected    string
	}{
		{
			etcdVersion: "3.5.7",
			expected:    "etcdctl",
		},
		{
			etcdVersion: "3.6.0",
			expected:    "etcdutl",
		},
		{
			etcdVersion: "3.6.6",
			expected:    "etcdutl",
		},
		{
			etcdVersion: "v3.6.6",
			expected:    "etcdutl",
		},
		{
			etcdVersion: "unknown",
			expected:    "etcdctl",
		},
	}

	for _, g := range grid {
		t.Run(g.etcdVersion, func(t *testing.T) {
			p := &etcdProcess{EtcdVersion: g.etcdVersion}
			if actual := p.snapshotRestoreCommand(); actual != g.expected {
				t.Fatalf("expected %q, got %q", g.expected, actual)
			}
		})
	}
}
