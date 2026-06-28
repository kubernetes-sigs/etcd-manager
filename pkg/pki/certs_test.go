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

package pki

import (
	"bytes"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	certutil "k8s.io/client-go/util/cert"
)

// TestCertMinTimeLeftLessThanDuration guards against CertMinTimeLeft >= CertDuration, which makes
// every freshly-issued certificate look like it is expiring and forces a re-sign on every start.
func TestCertMinTimeLeftLessThanDuration(t *testing.T) {
	if CertMinTimeLeft >= CertDuration {
		t.Fatalf("CertMinTimeLeft (%s) must be less than CertDuration (%s); otherwise valid certificates are reissued on every start", CertMinTimeLeft, CertDuration)
	}
}

// TestEnsureKeypairReusesCertAndKey verifies that a still-valid cert and its key are reused (not
// regenerated or re-signed) when the same CA is presented again, and that a CA rotation reissues
// the certificate while still reusing the (CA-independent) private key.
func TestEnsureKeypairReusesCertAndKey(t *testing.T) {
	SetRSAKeySize(2048)
	t.Cleanup(func() { SetRSAKeySize(4096) })

	dir := t.TempDir()
	ca, err := NewCA(NewInMemoryStore())
	if err != nil {
		t.Fatalf("building CA: %v", err)
	}

	config := certutil.Config{
		CommonName: "test-client",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	keyPath := filepath.Join(dir, "test-client.key")
	crtPath := filepath.Join(dir, "test-client.crt")

	// First call generates the key and signs the certificate.
	kp1, err := NewKeypairs(NewFSStore(dir), ca).EnsureKeypair("test-client", config)
	if err != nil {
		t.Fatalf("first EnsureKeypair: %v", err)
	}
	key1 := mustReadFile(t, keyPath)
	crt1 := mustReadFile(t, crtPath)
	verifyCertAgainstCA(t, kp1.Certificate, ca, x509.ExtKeyUsageClientAuth)

	// Second call with a fresh Keypairs over the same on-disk store and CA models an etcd restart:
	// it must reuse both the key and the certificate.
	kp2, err := NewKeypairs(NewFSStore(dir), ca).EnsureKeypair("test-client", config)
	if err != nil {
		t.Fatalf("second EnsureKeypair: %v", err)
	}
	key2 := mustReadFile(t, keyPath)
	crt2 := mustReadFile(t, crtPath)

	if !bytes.Equal(key1, key2) {
		t.Errorf("private key was regenerated when it should have been reused")
	}
	if !bytes.Equal(crt1, crt2) {
		t.Errorf("certificate was re-signed when it should have been reused")
	}
	if !kp1.Certificate.Equal(kp2.Certificate) {
		t.Errorf("returned certificate differs across calls")
	}

	// Rotate the CA: a new CA over the same store must reissue the cert (it no longer chains to the
	// old signer) while reusing the private key.
	ca2, err := NewCA(NewInMemoryStore())
	if err != nil {
		t.Fatalf("building rotated CA: %v", err)
	}
	kp3, err := NewKeypairs(NewFSStore(dir), ca2).EnsureKeypair("test-client", config)
	if err != nil {
		t.Fatalf("EnsureKeypair after CA rotation: %v", err)
	}
	key3 := mustReadFile(t, keyPath)
	crt3 := mustReadFile(t, crtPath)

	if !bytes.Equal(key2, key3) {
		t.Errorf("private key should be reused across a CA rotation")
	}
	if bytes.Equal(crt2, crt3) {
		t.Errorf("certificate was not reissued after a CA rotation")
	}
	// The reissued cert must validate against the new CA and must not validate against the old one.
	verifyCertAgainstCA(t, kp3.Certificate, ca2, x509.ExtKeyUsageClientAuth)
	if _, err := kp3.Certificate.Verify(x509.VerifyOptions{
		Roots:     ca.CertPool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err == nil {
		t.Errorf("reissued certificate unexpectedly validates against the old CA")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %q: %v", path, err)
	}
	return b
}

func verifyCertAgainstCA(t *testing.T, cert *x509.Certificate, ca *CA, usage x509.ExtKeyUsage) {
	t.Helper()
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:     ca.CertPool(),
		KeyUsages: []x509.ExtKeyUsage{usage},
	}); err != nil {
		t.Errorf("certificate %q failed to verify against CA: %v", cert.Subject.CommonName, err)
	}
}
