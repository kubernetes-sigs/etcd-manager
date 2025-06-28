/*
Copyright 2021 The Kubernetes Authors.

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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path"
	"sort"
	"strings"
	"testing"
	"time"

	certutil "k8s.io/client-go/util/cert"
)

func TestFSStore_LoadCA(t *testing.T) {
	basicCert, basicKey, err := generateCertKey("kubernetes")
	if err != nil {
		t.Fatalf("failed to generate cert/key: %v", err)
	}
	secondaryCert, secondaryKey, err := generateCertKey("apiserver-aggregator-ca")
	if err != nil {
		t.Fatalf("failed to generate cert/key: %v", err)
	}

	tests := []struct {
		name           string
		cert           string
		key            string
		expectedPool   string
		expectedBundle string
		expectedErr    string
	}{
		{
			name:           "basic",
			cert:           basicCert,
			key:            basicKey,
			expectedPool:   "CN=kubernetes",
			expectedBundle: basicCert,
		},
		{
			name:           "with_secondary",
			cert:           basicCert + secondaryCert,
			key:            basicKey,
			expectedPool:   "CN=apiserver-aggregator-ca\nCN=kubernetes",
			expectedBundle: basicCert + secondaryCert,
		},
		{
			name:           "using_secondary",
			cert:           basicCert + secondaryCert,
			key:            secondaryKey,
			expectedPool:   "CN=apiserver-aggregator-ca\nCN=kubernetes",
			expectedBundle: basicCert + secondaryCert,
		},
		{
			name:        "badcert",
			cert:        "not a cert",
			key:         basicKey,
			expectedErr: "error parsing certificate data in ",
		},
		{
			name:        "badkey",
			cert:        basicCert,
			key:         "not a key",
			expectedErr: "unable to parse private key ",
		},
		{
			name:        "no_matching_cert",
			cert:        basicCert,
			key:         secondaryKey,
			expectedErr: "did not find certificate for private key test-ca",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "test")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer func() {
				if os.Getenv("KEEP_TEMP_DIR") != "" {
					t.Logf("NOT removing temp directory, because KEEP_TEMP_DIR is set: %s", tempDir)
				} else {
					err := os.RemoveAll(tempDir)
					if err != nil {
						t.Fatalf("failed to remove temp dir %q: %v", tempDir, err)
					}
				}
			}()

			if tc.cert != "" {
				_ = os.WriteFile(path.Join(tempDir, "test-ca.crt"), []byte(tc.cert), 0400)
			}
			if tc.key != "" {
				_ = os.WriteFile(path.Join(tempDir, "test-ca.key"), []byte(tc.key), 0400)
			}

			store := NewFSStore(tempDir)
			actual, err := store.LoadCA("test-ca")
			if err != nil && tc.expectedErr == "" {
				t.Fatalf("unexpected error %v", err)
			}
			if err != nil && !strings.Contains(err.Error(), tc.expectedErr) {
				t.Fatalf("error = %v, expected %s", err, tc.expectedErr)
			}
			if err != nil {
				return
			}
			if tc.expectedErr != "" {
				t.Fatalf("did not get expected error %s", tc.expectedErr)
			}

			var subjects []string
			for _, subject := range actual.CertPool().Subjects() {
				var name pkix.RDNSequence
				rest, err := asn1.Unmarshal(subject, &name)
				if err != nil {
					t.Fatalf("subject unmarshal error %v", err)
				}
				if len(rest) > 0 {
					t.Fatalf("extra data after unmarshalling subject")
				}
				subjects = append(subjects, name.String())
			}
			sort.Strings(subjects)
			if strings.Join(subjects, "\n") != tc.expectedPool {
				t.Fatalf("unexpected pool subjects %s, expected %s", strings.Join(subjects, "\n"), tc.expectedPool)
			}

			err = store.WriteCABundle(actual)
			if err != nil {
				t.Errorf("writing CA bundle: %v", err)
			} else {
				bytes, err := os.ReadFile(path.Join(tempDir, "ca.crt"))
				if err != nil {
					t.Errorf("writing CA bundle: %v", err)
				} else if string(bytes) != tc.expectedBundle {
					t.Errorf("unexpected bundle. actual:\n%s\nexpected:\n%s\n", string(bytes), tc.expectedBundle)
				}
			}

			keypairs := NewKeypairs(NewInMemoryStore(), actual)
			keypair, err := keypairs.EnsureKeypair("test-cert", certutil.Config{
				CommonName: "test-cert",
				Usages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			})
			if err != nil {
				t.Errorf("ensuring keypair: %s", err)
			} else if keypair.Certificate.Subject.CommonName != "test-cert" {
				t.Errorf("unexpected subject")
			}
		})
	}
}

func generateCertKey(commonName string) (string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		},
	)

	basicCaTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	cert, err := x509.CreateCertificate(rand.Reader, basicCaTemplate, basicCaTemplate, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate certificate: %w", err)
	}
	certPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert,
		},
	)

	return string(certPEM), string(keyPEM), nil
}
