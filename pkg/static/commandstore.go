/*
Copyright 2024 The Kubernetes Authors.

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

package static

import (
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/klog/v2"
	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/commands"
)

// newClusterMarkerFile is the name of the marker file for a new-cluster.
// To avoid deleting data, we will only create a new cluster if this file exists.
// After the cluster is created, we will remove this file.
const newClusterMarkerFile = "please-create-new-cluster"

func NewStaticCommandStore(config *Config, dataDir string) *StaticStore {
	return &StaticStore{
		config:  config,
		dataDir: dataDir,
	}
}

type StaticStore struct {
	config *Config

	// dataDir is the location of our data files, it is used for IsNewCluster
	dataDir string
}

var _ commands.Store = &StaticStore{}

func (s *StaticStore) AddCommand(cmd *protoetcd.Command) error {
	return fmt.Errorf("StaticStore::AddCommand not supported")
}

func (s *StaticStore) ListCommands() ([]commands.Command, error) {
	klog.Infof("StaticStore::ListCommands returning empty list")
	return nil, nil
}

func (s *StaticStore) RemoveCommand(command commands.Command) error {
	return fmt.Errorf("StaticStore::RemoveCommand not supported")
}

func (s *StaticStore) GetExpectedClusterSpec() (*protoetcd.ClusterSpec, error) {
	spec := &protoetcd.ClusterSpec{
		MemberCount: int32(len(s.config.Nodes)),
		EtcdVersion: s.config.EtcdVersion,
	}
	return spec, nil
}

func (s *StaticStore) SetExpectedClusterSpec(spec *protoetcd.ClusterSpec) error {
	klog.Warningf("ignoring SetExpectedClusterSpec %v", spec)
	return nil
}

func (s *StaticStore) IsNewCluster() (bool, error) {
	markerPath := filepath.Join(s.dataDir, newClusterMarkerFile)
	_, err := os.Stat(markerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking for file %q: %w", markerPath, err)
	}

	// File exists so we can create a new cluster
	return true, nil
}

func (s *StaticStore) MarkClusterCreated() error {
	markerPath := filepath.Join(s.dataDir, newClusterMarkerFile)
	if err := os.Remove(markerPath); err != nil {
		return fmt.Errorf("deleting marker file %q: %w", markerPath, err)
	}
	return nil
}
