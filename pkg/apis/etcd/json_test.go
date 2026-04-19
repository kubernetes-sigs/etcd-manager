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
	"testing"

	"google.golang.org/protobuf/proto"
)

// TestRoundTrip verifies that messages persisted by the VFS call sites
// (BackupInfo in _backup.meta, Command and ClusterSpec in the commands
// store) survive a ToJson / FromJson round-trip with no data loss.
func TestRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		msg      proto.Message
		newEmpty func() proto.Message
	}{
		{
			name:     "ClusterSpec",
			msg:      &ClusterSpec{MemberCount: 3, EtcdVersion: "3.4.13"},
			newEmpty: func() proto.Message { return &ClusterSpec{} },
		},
		{
			name: "BackupInfo",
			msg: &BackupInfo{
				EtcdVersion: "3.4.13",
				Timestamp:   1704067200,
				ClusterSpec: &ClusterSpec{MemberCount: 3, EtcdVersion: "3.4.13"},
			},
			newEmpty: func() proto.Message { return &BackupInfo{} },
		},
		{
			name: "Command_RestoreBackup",
			msg: &Command{
				Timestamp: 1704067200,
				RestoreBackup: &RestoreBackupCommand{
					ClusterSpec: &ClusterSpec{MemberCount: 3, EtcdVersion: "3.4.13"},
					Backup:      "2024-01-01T00-00-00Z-1",
				},
			},
			newEmpty: func() proto.Message { return &Command{} },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ToJson(tc.msg)
			if err != nil {
				t.Fatalf("ToJson: %v", err)
			}
			if s == "" {
				t.Fatalf("ToJson returned empty string")
			}
			got := tc.newEmpty()
			if err := FromJson(s, got); err != nil {
				t.Fatalf("FromJson: %v", err)
			}
			if !proto.Equal(tc.msg, got) {
				t.Fatalf("round-trip mismatch\n  got:  %v\n  want: %v", got, tc.msg)
			}
		})
	}
}

// TestParsesLegacyJsonpbFormat pins down backward compatibility: JSON produced
// by the previous jsonpb.Marshaler{Indent: "  "} implementation - which is
// what is on disk in any currently-deployed backup store - must still be
// readable by the new protojson-based FromJson.
//
// The fixtures below are byte-for-byte representative of what jsonpb produced
// for each message type: lowerCamelCase field names (OrigName=false default),
// int64 fields quoted as strings (per proto3 JSON spec), unset fields omitted
// (EmitDefaults=false default).
func TestParsesLegacyJsonpbFormat(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		newEmpty func() proto.Message
		want     proto.Message
	}{
		{
			name: "ClusterSpec",
			input: `{
  "memberCount": 3,
  "etcdVersion": "3.4.13"
}`,
			newEmpty: func() proto.Message { return &ClusterSpec{} },
			want:     &ClusterSpec{MemberCount: 3, EtcdVersion: "3.4.13"},
		},
		{
			name: "BackupInfo",
			input: `{
  "etcdVersion": "3.4.13",
  "timestamp": "1704067200",
  "clusterSpec": {
    "memberCount": 3,
    "etcdVersion": "3.4.13"
  }
}`,
			newEmpty: func() proto.Message { return &BackupInfo{} },
			want: &BackupInfo{
				EtcdVersion: "3.4.13",
				Timestamp:   1704067200,
				ClusterSpec: &ClusterSpec{MemberCount: 3, EtcdVersion: "3.4.13"},
			},
		},
		{
			name: "Command_RestoreBackup",
			input: `{
  "timestamp": "1704067200",
  "restoreBackup": {
    "clusterSpec": {
      "memberCount": 3,
      "etcdVersion": "3.4.13"
    },
    "backup": "2024-01-01T00-00-00Z-1"
  }
}`,
			newEmpty: func() proto.Message { return &Command{} },
			want: &Command{
				Timestamp: 1704067200,
				RestoreBackup: &RestoreBackupCommand{
					ClusterSpec: &ClusterSpec{MemberCount: 3, EtcdVersion: "3.4.13"},
					Backup:      "2024-01-01T00-00-00Z-1",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.newEmpty()
			if err := FromJson(tc.input, got); err != nil {
				t.Fatalf("FromJson: %v", err)
			}
			if !proto.Equal(tc.want, got) {
				t.Fatalf("legacy-format mismatch\n  got:  %v\n  want: %v", got, tc.want)
			}
		})
	}
}
