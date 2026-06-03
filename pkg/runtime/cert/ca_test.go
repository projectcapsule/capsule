// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/projectcapsule/capsule/pkg/runtime/cert"
)

func TestNewCertificateAuthorityFromBytes(t *testing.T) {
	t.Parallel()

	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		t.Fatalf("expected CA generation to succeed, got %v", err)
	}

	crt, err := ca.CACertificatePem()
	if err != nil {
		t.Fatalf("expected CA certificate PEM encoding to succeed, got %v", err)
	}

	key, err := ca.CAPrivateKeyPem()
	if err != nil {
		t.Fatalf("expected CA private key PEM encoding to succeed, got %v", err)
	}

	loadedCA, err := cert.NewCertificateAuthorityFromBytes(crt.Bytes(), key.Bytes())
	if err != nil {
		t.Fatalf("expected loading CA from PEM bytes to succeed, got %v", err)
	}

	loadedCrt, err := loadedCA.CACertificatePem()
	if err != nil {
		t.Fatalf("expected loaded CA certificate PEM encoding to succeed, got %v", err)
	}

	if !bytes.Equal(crt.Bytes(), loadedCrt.Bytes()) {
		t.Fatal("expected loaded CA certificate PEM to match original CA certificate PEM")
	}
}

func TestNewCertificateAuthorityFromBytesRejectsInvalidCertificatePEM(t *testing.T) {
	t.Parallel()

	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		t.Fatalf("expected CA generation to succeed, got %v", err)
	}

	key, err := ca.CAPrivateKeyPem()
	if err != nil {
		t.Fatalf("expected CA private key PEM encoding to succeed, got %v", err)
	}

	if _, err := cert.NewCertificateAuthorityFromBytes([]byte("invalid certificate"), key.Bytes()); err == nil {
		t.Fatal("expected invalid certificate PEM to be rejected")
	}
}

func TestNewCertificateAuthorityFromBytesRejectsInvalidPrivateKeyPEM(t *testing.T) {
	t.Parallel()

	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		t.Fatalf("expected CA generation to succeed, got %v", err)
	}

	crt, err := ca.CACertificatePem()
	if err != nil {
		t.Fatalf("expected CA certificate PEM encoding to succeed, got %v", err)
	}

	if _, err := cert.NewCertificateAuthorityFromBytes(crt.Bytes(), []byte("invalid private key")); err == nil {
		t.Fatal("expected invalid private key PEM to be rejected")
	}
}

func TestCapsuleCAGenerateCertificate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		sans cert.CertificateSANs
	}{
		"dns name": {
			sans: cert.CertificateSANs{
				DNSNames: []string{"foo.tld"},
			},
		},
		"multiple dns names": {
			sans: cert.CertificateSANs{
				DNSNames: []string{
					"capsule-webhook-service.capsule-system.svc",
					"capsule-webhook-service.capsule-system.svc.cluster.local",
				},
			},
		},
		"dns names are normalized": {
			sans: cert.CertificateSANs{
				DNSNames: []string{
					" Capsule-Webhook-Service.Capsule-System.Svc ",
					"capsule-webhook-service.capsule-system.svc",
				},
			},
		},
		"ip addresses": {
			sans: cert.CertificateSANs{
				IPAddrs: []net.IP{
					net.ParseIP("10.96.0.10"),
					net.ParseIP("fd00::1"),
				},
			},
		},
		"dns names and ip addresses": {
			sans: cert.CertificateSANs{
				DNSNames: []string{
					"capsule-webhook-service.capsule-system.svc",
				},
				IPAddrs: []net.IP{
					net.ParseIP("10.96.0.10"),
				},
			},
		},
	}

	for name, tt := range tests {
		tt := tt

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			expirationDate := time.Now().AddDate(1, 0, 0).UTC()

			ca, err := cert.GenerateCertificateAuthority()
			if err != nil {
				t.Fatalf("expected CA generation to succeed, got %v", err)
			}

			crt, key, err := ca.GenerateCertificate(cert.NewCertOpts(expirationDate, tt.sans))
			if err != nil {
				t.Fatalf("expected serving certificate generation to succeed, got %v", err)
			}

			servingCert := parseCertificatePEM(t, crt.Bytes())

			if servingCert.NotAfter.Unix() != expirationDate.Unix() {
				t.Fatalf("expected certificate NotAfter %d, got %d", expirationDate.Unix(), servingCert.NotAfter.Unix())
			}

			expectedSANs := tt.sans.Normalize()
			actualSANs := cert.CertificateSANs{
				DNSNames: servingCert.DNSNames,
				IPAddrs:  servingCert.IPAddresses,
			}.Normalize()

			assertCertificateSANsEqual(t, expectedSANs, actualSANs)

			if !tt.sans.MatchesCertificate(servingCert) {
				t.Fatalf(
					"expected generated certificate to match requested SANs, desiredDNSNames=%v desiredIPAddresses=%v actualDNSNames=%v actualIPAddresses=%v",
					expectedSANs.DNSNames,
					cert.IPsToStrings(expectedSANs.IPAddrs),
					servingCert.DNSNames,
					cert.IPsToStrings(servingCert.IPAddresses),
				)
			}

			if _, err := tls.X509KeyPair(crt.Bytes(), key.Bytes()); err != nil {
				t.Fatalf("expected generated certificate/key pair to be valid, got %v", err)
			}
		})
	}
}

func TestCapsuleCAGenerateCertificateIsSignedByCA(t *testing.T) {
	t.Parallel()

	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		t.Fatalf("expected CA generation to succeed, got %v", err)
	}

	expirationDate := time.Now().AddDate(1, 0, 0).UTC()

	crt, _, err := ca.GenerateCertificate(cert.NewCertOpts(expirationDate, cert.CertificateSANs{
		DNSNames: []string{"capsule-webhook-service.capsule-system.svc"},
	}))
	if err != nil {
		t.Fatalf("expected serving certificate generation to succeed, got %v", err)
	}

	servingCert := parseCertificatePEM(t, crt.Bytes())

	caCrt, err := ca.CACertificatePem()
	if err != nil {
		t.Fatalf("expected CA certificate PEM encoding to succeed, got %v", err)
	}

	caCert := parseCertificatePEM(t, caCrt.Bytes())

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	verifyOptions := x509.VerifyOptions{
		DNSName: "capsule-webhook-service.capsule-system.svc",
		Roots:   roots,
	}

	if _, err := servingCert.Verify(verifyOptions); err != nil {
		t.Fatalf("expected serving certificate to verify against generated CA, got %v", err)
	}
}

func TestCapsuleCAGenerateCertificateWithIPAddressSANVerifiesForIP(t *testing.T) {
	t.Parallel()

	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		t.Fatalf("expected CA generation to succeed, got %v", err)
	}

	ip := net.ParseIP("10.96.0.10")
	expirationDate := time.Now().AddDate(1, 0, 0).UTC()

	crt, _, err := ca.GenerateCertificate(cert.NewCertOpts(expirationDate, cert.CertificateSANs{
		IPAddrs: []net.IP{ip},
	}))
	if err != nil {
		t.Fatalf("expected serving certificate generation to succeed, got %v", err)
	}

	servingCert := parseCertificatePEM(t, crt.Bytes())

	caCrt, err := ca.CACertificatePem()
	if err != nil {
		t.Fatalf("expected CA certificate PEM encoding to succeed, got %v", err)
	}

	caCert := parseCertificatePEM(t, caCrt.Bytes())

	roots := x509.NewCertPool()
	roots.AddCert(caCert)

	verifyOptions := x509.VerifyOptions{
		DNSName: ip.String(),
		Roots:   roots,
	}

	if _, err := servingCert.Verify(verifyOptions); err != nil {
		t.Fatalf("expected serving certificate to verify against generated CA for IP SAN, got %v", err)
	}
}

func parseCertificatePEM(t *testing.T, certificatePEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		t.Fatal("expected certificate PEM block, got nil")
	}

	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("expected certificate parsing to succeed, got %v", err)
	}

	return certificate
}

func assertCertificateSANsEqual(t *testing.T, expected, actual cert.CertificateSANs) {
	t.Helper()

	expected = expected.Normalize()
	actual = actual.Normalize()

	if !reflect.DeepEqual(expected.DNSNames, actual.DNSNames) {
		t.Fatalf("expected DNS names %v, got %v", expected.DNSNames, actual.DNSNames)
	}

	expectedIPs := cert.IPsToStrings(expected.IPAddrs)
	actualIPs := cert.IPsToStrings(actual.IPAddrs)

	if !reflect.DeepEqual(expectedIPs, actualIPs) {
		t.Fatalf("expected IP addresses %v, got %v", expectedIPs, actualIPs)
	}
}
