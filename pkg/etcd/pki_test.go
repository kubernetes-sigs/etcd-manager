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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	protoetcd "sigs.k8s.io/etcd-manager/pkg/apis/etcd"
	"sigs.k8s.io/etcd-manager/pkg/pki"
)

// TestCreateKeypairsReuse verifies that a second createKeypairs call with identical inputs and
// the same pkiDir (an etcd restart) regenerates no key and re-signs no certificate, and that the
// persisted keypairs stay valid for etcd's peer and client TLS roles.
func TestCreateKeypairsReuse(t *testing.T) {
	peersCA, clientsCA := setupTestCAs(t)
	pkiDir := t.TempDir()

	me := &protoetcd.EtcdNode{
		Name: "node1",
		// Peer and client URLs share a host, so the cert gets duplicate SAN DNS names (as in
		// production); this checks the reuse/match logic is stable in that case.
		PeerUrls:   []string{"https://node1:2380"},
		ClientUrls: []string{"https://node1:2379"},
	}
	peerClientIPs := []net.IP{net.ParseIP("10.0.0.1")}

	// First start generates the peer ("me"), client-serving ("server") and client keypairs.
	if err := (&etcdProcess{}).createKeypairs(peersCA, clientsCA, pkiDir, me, peerClientIPs); err != nil {
		t.Fatalf("first createKeypairs: %v", err)
	}

	files := []string{
		filepath.Join("peers", "me.key"),
		filepath.Join("peers", "me.crt"),
		filepath.Join("clients", "server.key"),
		filepath.Join("clients", "server.crt"),
		filepath.Join("clients", "client.key"),
		filepath.Join("clients", "client.crt"),
	}

	// The client keypair must be persisted under pkiDir; it used to be held in an in-memory
	// store and regenerated on every start.
	if _, err := os.Stat(filepath.Join(pkiDir, "clients", "client.key")); err != nil {
		t.Fatalf("client key was not persisted under pkiDir: %v", err)
	}

	before := make(map[string][]byte, len(files))
	for _, f := range files {
		before[f] = mustReadFile(t, filepath.Join(pkiDir, f))
	}

	// Second start with identical inputs and the same pkiDir must not regenerate any key or
	// re-sign any certificate.
	p2 := &etcdProcess{}
	if err := p2.createKeypairs(peersCA, clientsCA, pkiDir, me, peerClientIPs); err != nil {
		t.Fatalf("second createKeypairs: %v", err)
	}
	for _, f := range files {
		after := mustReadFile(t, filepath.Join(pkiDir, f))
		if !bytes.Equal(before[f], after) {
			t.Errorf("%s changed between calls: key was regenerated or certificate re-signed", f)
		}
	}

	// Each cert must validate against its CA for the TLS roles etcd needs: the peer and server
	// certs serve and dial; the client cert is what etcd-manager uses to talk to etcd.
	verifyCertFile(t, filepath.Join(pkiDir, "peers", "me.crt"), peersCA, x509.ExtKeyUsageServerAuth)
	verifyCertFile(t, filepath.Join(pkiDir, "peers", "me.crt"), peersCA, x509.ExtKeyUsageClientAuth)
	verifyCertFile(t, filepath.Join(pkiDir, "clients", "server.crt"), clientsCA, x509.ExtKeyUsageServerAuth)
	verifyCertFile(t, filepath.Join(pkiDir, "clients", "server.crt"), clientsCA, x509.ExtKeyUsageClientAuth)
	verifyCertFile(t, filepath.Join(pkiDir, "clients", "client.crt"), clientsCA, x509.ExtKeyUsageClientAuth)

	// The TLS config used to dial etcd must be populated, and its leaf must be the persisted
	// client certificate, valid for client auth.
	if p2.etcdClientTLSConfig == nil || len(p2.etcdClientTLSConfig.Certificates) == 0 {
		t.Fatalf("etcdClientTLSConfig was not populated")
	}
	leaf := p2.etcdClientTLSConfig.Certificates[0].Leaf
	if leaf == nil {
		t.Fatalf("etcdClientTLSConfig leaf certificate not set")
	}
	verifyCert(t, leaf, clientsCA, x509.ExtKeyUsageClientAuth)
}

// TestCreateKeypairsServeMutualTLS confirms the persisted (reused) server and client certs
// complete a real mutual TLS handshake the way etcd uses them: the server serves with the "server"
// keypair and verifies the "client" keypair against the clients CA. This exercises key/cert
// pairing, certificate chaining, and the ServerAuth/ClientAuth usages without an etcd binary.
func TestCreateKeypairsServeMutualTLS(t *testing.T) {
	peersCA, clientsCA := setupTestCAs(t)
	pkiDir := t.TempDir()

	me := &protoetcd.EtcdNode{
		Name:       "node1",
		PeerUrls:   []string{"https://127.0.0.1:2380"},
		ClientUrls: []string{"https://127.0.0.1:2379"},
	}
	if err := (&etcdProcess{}).createKeypairs(peersCA, clientsCA, pkiDir, me, nil); err != nil {
		t.Fatalf("createKeypairs: %v", err)
	}

	// LoadX509KeyPair also confirms each persisted key matches its certificate.
	serverPair, err := tls.LoadX509KeyPair(
		filepath.Join(pkiDir, "clients", "server.crt"),
		filepath.Join(pkiDir, "clients", "server.key"))
	if err != nil {
		t.Fatalf("loading persisted server keypair: %v", err)
	}
	clientPair, err := tls.LoadX509KeyPair(
		filepath.Join(pkiDir, "clients", "client.crt"),
		filepath.Join(pkiDir, "clients", "client.key"))
	if err != nil {
		t.Fatalf("loading persisted client keypair: %v", err)
	}

	caPool := clientsCA.CertPool()

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverPair},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	srv.StartTLS()
	defer srv.Close()

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:      caPool,
				Certificates: []tls.Certificate{clientPair},
			},
		},
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("mutual TLS handshake with reused etcd certs failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status from TLS server: %d", resp.StatusCode)
	}
}

// setupTestCAs shrinks the RSA key size for speed (restoring it afterwards) and builds the peer
// and client CAs, which in production are loaded from disk and stable across restarts.
func setupTestCAs(t *testing.T) (peersCA, clientsCA *pki.CA) {
	t.Helper()
	pki.SetRSAKeySize(2048)
	t.Cleanup(func() { pki.SetRSAKeySize(4096) })

	peersCA, err := pki.NewCA(pki.NewInMemoryStore())
	if err != nil {
		t.Fatalf("building peers CA: %v", err)
	}
	clientsCA, err = pki.NewCA(pki.NewInMemoryStore())
	if err != nil {
		t.Fatalf("building clients CA: %v", err)
	}
	return peersCA, clientsCA
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %q: %v", path, err)
	}
	return b
}

func verifyCertFile(t *testing.T, path string, ca *pki.CA, usage x509.ExtKeyUsage) {
	t.Helper()
	cert, err := pki.ParseOneCertificate(mustReadFile(t, path))
	if err != nil {
		t.Fatalf("parsing %q: %v", path, err)
	}
	verifyCert(t, cert, ca, usage)
}

func verifyCert(t *testing.T, cert *x509.Certificate, ca *pki.CA, usage x509.ExtKeyUsage) {
	t.Helper()
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     ca.CertPool(),
		KeyUsages: []x509.ExtKeyUsage{usage},
	}); err != nil {
		t.Errorf("certificate %q failed to verify against CA (usage %v): %v", cert.Subject.CommonName, usage, err)
	}
}
