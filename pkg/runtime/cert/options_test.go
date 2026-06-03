// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import (
	"net"
	"testing"
	"time"
)

func TestNewCertOpts(t *testing.T) {
	t.Parallel()

	expirationDate := time.Now().Add(24 * time.Hour).UTC()

	opts := NewCertOpts(expirationDate, CertificateSANs{
		DNSNames: []string{
			" Webhook.Capsule-System.Svc ",
			"webhook.capsule-system.svc",
			"api.example.com",
		},
		IPAddrs: []net.IP{
			net.ParseIP("10.96.0.10"),
			net.ParseIP("10.96.0.10"),
			nil,
		},
	})

	if !opts.GetExpirationDate().Equal(expirationDate) {
		t.Fatalf("expected expiration date %s, got %s", expirationDate, opts.GetExpirationDate())
	}

	expectedSANs := CertificateSANs{
		DNSNames: []string{
			"api.example.com",
			"webhook.capsule-system.svc",
		},
		IPAddrs: []net.IP{
			net.ParseIP("10.96.0.10"),
		},
	}

	assertCertificateSANsEqual(t, expectedSANs, opts.GetDNSNames())
}

func TestCertOptsImplementsCertificateOptions(t *testing.T) {
	t.Parallel()

	var _ CertificateOptions = CertOpts{}
}
