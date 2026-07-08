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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	certutil "k8s.io/client-go/util/cert"
)

func TestNewCAIsRSA(t *testing.T) {
	SetRSAKeySize(2048)

	ca, err := NewCA(NewInMemoryStore())
	if err != nil {
		t.Fatalf("failed to create CA: %v", err)
	}
	if _, ok := ca.privateKey.(*rsa.PrivateKey); !ok {
		t.Fatalf("expected RSA CA private key, got %T", ca.privateKey)
	}
}

func TestEnsureKeypairGeneratesECDSA(t *testing.T) {
	SetRSAKeySize(2048)

	ca, err := NewCA(NewInMemoryStore())
	if err != nil {
		t.Fatalf("failed to create CA: %v", err)
	}

	keypairs := NewKeypairs(NewInMemoryStore(), ca)
	keypair, err := keypairs.EnsureKeypair("test-cert", certutil.Config{
		CommonName: "test-cert",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		t.Fatalf("ensuring keypair: %v", err)
	}

	ecdsaKey, ok := keypair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("expected ECDSA private key, got %T", keypair.PrivateKey)
	}
	if ecdsaKey.Curve != elliptic.P256() {
		t.Fatalf("expected P-256 curve, got %v", ecdsaKey.Curve)
	}
	if !ecdsaKey.PublicKey.Equal(keypair.Certificate.PublicKey) {
		t.Fatalf("certificate public key does not match private key")
	}
	if keypair.Certificate.KeyUsage != x509.KeyUsageDigitalSignature {
		t.Fatalf("unexpected key usage %v", keypair.Certificate.KeyUsage)
	}
}

// TestEnsureKeypairMigratesRSAToECDSA verifies that a pre-existing RSA leaf
// keypair on disk (as written by older etcd-manager versions) is replaced
// with an ECDSA P-256 keypair, and that the reissued certificate still
// verifies against the (RSA) CA.
func TestEnsureKeypairMigratesRSAToECDSA(t *testing.T) {
	SetRSAKeySize(2048)

	tempDir := t.TempDir()

	ca, err := NewCA(NewInMemoryStore())
	if err != nil {
		t.Fatalf("failed to create CA: %v", err)
	}

	config := certutil.Config{
		CommonName: "test-cert",
		Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	// Write an RSA leaf keypair in the legacy on-disk format
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate RSA key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})
	if err := os.WriteFile(filepath.Join(tempDir, "test-cert.key"), keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}
	rsaCert, err := newSignedCert(&config, rsaKey, ca, CertDuration)
	if err != nil {
		t.Fatalf("failed to sign RSA certificate: %v", err)
	}
	if err := writeCertificates(filepath.Join(tempDir, "test-cert.crt"), rsaCert); err != nil {
		t.Fatalf("failed to write certificate: %v", err)
	}

	keypairs := NewKeypairs(NewFSStore(tempDir), ca)
	keypair, err := keypairs.EnsureKeypair("test-cert", config)
	if err != nil {
		t.Fatalf("ensuring keypair: %v", err)
	}

	ecdsaKey, ok := keypair.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("expected ECDSA private key after migration, got %T", keypair.PrivateKey)
	}
	if ecdsaKey.Curve != elliptic.P256() {
		t.Fatalf("expected P-256 curve, got %v", ecdsaKey.Curve)
	}
	if !ecdsaKey.PublicKey.Equal(keypair.Certificate.PublicKey) {
		t.Fatalf("certificate public key does not match private key")
	}

	if _, err := keypair.Certificate.Verify(x509.VerifyOptions{
		Roots:     ca.CertPool(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("certificate does not verify against CA: %v", err)
	}

	// The key on disk should round-trip as the same ECDSA key
	loadedKey, err := loadPrivateKey(filepath.Join(tempDir, "test-cert.key"))
	if err != nil {
		t.Fatalf("loading private key: %v", err)
	}
	if !privateKeysEqual(loadedKey, keypair.PrivateKey) {
		t.Fatalf("key on disk does not match returned key")
	}

	// A subsequent EnsureKeypair must reuse the ECDSA key
	keypair2, err := keypairs.EnsureKeypair("test-cert", config)
	if err != nil {
		t.Fatalf("ensuring keypair again: %v", err)
	}
	if !privateKeysEqual(keypair.PrivateKey, keypair2.PrivateKey) {
		t.Fatalf("private key was unexpectedly regenerated")
	}
}
