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

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
)

func TestIsDiskEmpty(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{
			name: "missing state data and trashcan",
			want: true,
		},
		{
			name: "empty data and trashcan dirs",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				mkdirAll(t, filepath.Join(dir, DataDirName))
				mkdirAll(t, filepath.Join(dir, TrashcanDirName))
			},
			want: true,
		},
		{
			name: "state exists",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, StateFileName))
			},
		},
		{
			name: "data has entry",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				mkdirAll(t, filepath.Join(dir, DataDirName, "cluster-token"))
			},
		},
		{
			name: "trashcan has entry",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				mkdirAll(t, filepath.Join(dir, TrashcanDirName, "cluster-token"))
			},
		},
		{
			name: "data path is file",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				writeFile(t, filepath.Join(dir, DataDirName))
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.setup != nil {
				test.setup(t, dir)
			}
			if got := isDiskEmpty(dir); got != test.want {
				t.Fatalf("isDiskEmpty() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGetInfoReportsDiskEmptyLive(t *testing.T) {
	dir := t.TempDir()
	server := &EtcdServer{
		baseDir:     dir,
		clusterName: "test",
		etcdNodeConfiguration: &protoetcd.EtcdNode{
			Name: "etcd-a",
		},
	}

	response, err := server.GetInfo(context.Background(), &protoetcd.GetInfoRequest{})
	if err != nil {
		t.Fatalf("GetInfo() returned error: %v", err)
	}
	if !response.DiskEmpty {
		t.Fatalf("GetInfo().DiskEmpty = false, want true")
	}

	mkdirAll(t, filepath.Join(dir, DataDirName, "cluster-token"))

	response, err = server.GetInfo(context.Background(), &protoetcd.GetInfoRequest{})
	if err != nil {
		t.Fatalf("GetInfo() returned error: %v", err)
	}
	if response.DiskEmpty {
		t.Fatalf("GetInfo().DiskEmpty = true after data was created, want false")
	}
}

func mkdirAll(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", p, err)
	}
}

func writeFile(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", p, err)
	}
}
