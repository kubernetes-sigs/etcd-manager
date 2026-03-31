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

package linode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTagSpec(t *testing.T) {
	tests := []struct {
		name      string
		tag       string
		wantKey   string
		wantValue string
		wantErr   bool
	}{
		{name: "key_only", tag: "kops.k8s.io/cluster", wantKey: "kops.k8s.io/cluster", wantValue: ""},
		{name: "colon", tag: "kops.k8s.io/cluster:example.k8s.local", wantKey: "kops.k8s.io/cluster", wantValue: "example.k8s.local"},
		{name: "equals", tag: "kops.k8s.io/cluster=example.k8s.local", wantKey: "kops.k8s.io/cluster", wantValue: "example.k8s.local"},
		{name: "empty", tag: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotValue, err := parseTagSpec(tt.tag)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if gotKey != tt.wantKey {
				t.Fatalf("key = %q, want %q", gotKey, tt.wantKey)
			}
			if gotValue != tt.wantValue {
				t.Fatalf("value = %q, want %q", gotValue, tt.wantValue)
			}
		})
	}
}

func TestTagsMatch(t *testing.T) {
	resourceTags := []string{
		"kops.k8s.io/cluster:example.k8s.local",
		"kops.k8s.io/etcd:main",
		"kops.k8s.io/instance-group=control-plane-us-ord",
	}

	required := map[string]string{
		"kops.k8s.io/cluster":        "example.k8s.local",
		"kops.k8s.io/etcd":           "main",
		"kops.k8s.io/instance-group": "control-plane-us-ord",
	}

	if !tagsMatch(resourceTags, required) {
		t.Fatalf("expected tags to match")
	}

	required["kops.k8s.io/instance-role"] = "control-plane"
	if tagsMatch(resourceTags, required) {
		t.Fatalf("expected tags not to match when required tag is missing")
	}
}

func TestLoadLinodeMetadata(t *testing.T) {
	const expectedToken = "abc123"

	h := http.NewServeMux()
	h.HandleFunc(linodeMetadataTokenPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("unexpected method %q", r.Method)
		}
		if got := r.Header.Get("Metadata-Token-Expiry-Seconds"); got == "" {
			t.Fatalf("expected Metadata-Token-Expiry-Seconds header")
		}
		fmt.Fprint(w, expectedToken)
	})
	h.HandleFunc(linodeMetadataInstancePath, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Metadata-Token"); got != expectedToken {
			t.Fatalf("unexpected Metadata-Token %q", got)
		}
		fmt.Fprint(w, "id: 42\nlabel: test-node\nregion: us-ord\n")
	})

	ts := httptest.NewServer(h)
	defer ts.Close()

	instanceID, region, err := loadLinodeMetadata(context.Background(), ts.Client(), ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if instanceID != 42 {
		t.Fatalf("instanceID = %d, want 42", instanceID)
	}
	if region != "us-ord" {
		t.Fatalf("region = %q, want us-ord", region)
	}
}
