/*
Copyright 2019 The Kubernetes Authors.

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
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math"
	"math/big"
	"time"

	certutil "k8s.io/client-go/util/cert"
)

const (
	// CertificateBlockType is a possible value for pem.Block.Type.
	CertificateBlockType = "CERTIFICATE"
)

var rsaKeySize = 4096

// SetRSAKeySize overrides the RSA key size used for certificate generation, for use in testing.
func SetRSAKeySize(size int) {
	rsaKeySize = size
}

// newCAPrivateKey creates an RSA private key, used for CAs
func newCAPrivateKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(cryptorand.Reader, rsaKeySize)
}

// newLeafPrivateKey creates an ECDSA P-256 private key, used for leaf certificates
func newLeafPrivateKey() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
}

// newSignedCert creates a signed certificate using the given CA.
func newSignedCert(cfg *certutil.Config, key crypto.Signer, ca *CA, duration time.Duration) (*x509.Certificate, error) {
	caCert := ca.primaryCertificate
	caKey := ca.privateKey
	serial, err := cryptorand.Int(cryptorand.Reader, new(big.Int).SetInt64(math.MaxInt64))
	if err != nil {
		return nil, err
	}
	if len(cfg.CommonName) == 0 {
		return nil, fmt.Errorf("must specify a CommonName")
	}
	if len(cfg.Usages) == 0 {
		return nil, fmt.Errorf("must specify at least one ExtKeyUsage")
	}

	keyUsage := x509.KeyUsageDigitalSignature
	if _, isRSA := key.Public().(*rsa.PublicKey); isRSA {
		// KeyEncipherment is only meaningful for RSA key transport
		keyUsage |= x509.KeyUsageKeyEncipherment
	}

	certTmpl := x509.Certificate{
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: cfg.Organization,
		},
		DNSNames:     cfg.AltNames.DNSNames,
		IPAddresses:  cfg.AltNames.IPs,
		SerialNumber: serial,
		NotBefore:    caCert.NotBefore,
		NotAfter:     time.Now().Add(duration).UTC(),
		KeyUsage:     keyUsage,
		ExtKeyUsage:  cfg.Usages,
	}
	certDERBytes, err := x509.CreateCertificate(cryptorand.Reader, &certTmpl, caCert, key.Public(), caKey)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(certDERBytes)
}
