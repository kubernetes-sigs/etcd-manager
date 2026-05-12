/*
Copyright 2020 The Kubernetes Authors.

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

package integration

import (
	"testing"

	"github.com/blang/semver/v4"
	"sigs.k8s.io/etcd-manager/pkg/etcd"
	"sigs.k8s.io/etcd-manager/pkg/etcdversions"
)

func TestEtcdInstalled(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	for _, etcdVersion := range etcdversions.LatestEtcdVersions {
		t.Run("etcdVersion="+etcdVersion, func(t *testing.T) {
			{
				bindir, err := etcd.BindirForEtcdVersion(etcdVersion, "etcd")
				if err != nil {
					t.Errorf("etcd %q not installed in /opt: %v", etcdVersion, err)
				}
				if bindir == "" {
					t.Errorf("etcd %q did not return bindir", etcdVersion)
				}
			}
			{
				bindir, err := etcd.BindirForEtcdVersion(etcdVersion, "etcdctl")
				if err != nil {
					t.Errorf("etcdctl %q not installed in /opt: %v", etcdVersion, err)
				}
				if bindir == "" {
					t.Errorf("etcdctl %q did not return bindir", etcdVersion)
				}
			}
			// etcd 3.6 removed `etcdctl snapshot restore`; restores require etcdutl.
			if v, err := semver.ParseTolerant(etcdVersion); err == nil && (v.Major > 3 || (v.Major == 3 && v.Minor >= 6)) {
				bindir, err := etcd.BindirForEtcdVersion(etcdVersion, "etcdutl")
				if err != nil {
					t.Errorf("etcdutl %q not installed in /opt: %v", etcdVersion, err)
				}
				if bindir == "" {
					t.Errorf("etcdutl %q did not return bindir", etcdVersion)
				}
			}
		})
	}
}
