// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import (
	"crypto/x509"
	"net"
	"reflect"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/utils/ptr"
)

func TestCertificateSANsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sans CertificateSANs
		want bool
	}{
		{
			name: "empty",
			sans: CertificateSANs{},
			want: true,
		},
		{
			name: "dns names present",
			sans: CertificateSANs{
				DNSNames: []string{"webhook.capsule-system.svc"},
			},
			want: false,
		},
		{
			name: "ip addresses present",
			sans: CertificateSANs{
				IPAddrs: []net.IP{net.ParseIP("10.96.0.10")},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.sans.Empty(); got != tt.want {
				t.Fatalf("expected Empty() to be %t, got %t", tt.want, got)
			}
		})
	}
}

func TestCertificateSANsNormalize(t *testing.T) {
	t.Parallel()

	sans := CertificateSANs{
		DNSNames: []string{
			" Webhook.Capsule-System.Svc ",
			"",
			"webhook.capsule-system.svc",
			"API.Example.Com",
			" api.example.com ",
		},
		IPAddrs: []net.IP{
			net.ParseIP("10.96.0.20"),
			nil,
			net.ParseIP("10.96.0.10"),
			net.ParseIP("10.96.0.20"),
		},
	}

	expected := CertificateSANs{
		DNSNames: []string{
			"api.example.com",
			"webhook.capsule-system.svc",
		},
		IPAddrs: []net.IP{
			net.ParseIP("10.96.0.10"),
			net.ParseIP("10.96.0.20"),
		},
	}

	assertCertificateSANsEqual(t, expected, sans.Normalize())
}

func TestCertificateSANsAddDNSNames(t *testing.T) {
	t.Parallel()

	var sans CertificateSANs

	sans.AddDNSNames("webhook", "webhook.capsule-system.svc")

	expected := []string{
		"webhook",
		"webhook.capsule-system.svc",
	}

	if !reflect.DeepEqual(expected, sans.DNSNames) {
		t.Fatalf("expected DNS names %v, got %v", expected, sans.DNSNames)
	}
}

func TestCertificateSANsAddIPAddrs(t *testing.T) {
	t.Parallel()

	var sans CertificateSANs

	sans.AddIPAddrs(net.ParseIP("10.96.0.10"), net.ParseIP("fd00::1"))

	expected := []string{
		"10.96.0.10",
		"fd00::1",
	}

	if got := IPsToStrings(sans.IPAddrs); !reflect.DeepEqual(expected, got) {
		t.Fatalf("expected IP addresses %v, got %v", expected, got)
	}
}

func TestCertificateSANsAddServiceReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		service          *admissionregistrationv1.ServiceReference
		defaultNamespace string
		expected         []string
	}{
		{
			name: "nil service is ignored",
		},
		{
			name: "empty service name is ignored",
			service: &admissionregistrationv1.ServiceReference{
				Namespace: "capsule-system",
			},
		},
		{
			name: "uses service namespace",
			service: &admissionregistrationv1.ServiceReference{
				Name:      "capsule-webhook-service",
				Namespace: "capsule-system",
			},
			defaultNamespace: "default",
			expected: []string{
				"capsule-webhook-service",
				"capsule-webhook-service.capsule-system",
				"capsule-webhook-service.capsule-system.svc",
				"capsule-webhook-service.capsule-system.svc.cluster.local",
			},
		},
		{
			name: "uses default namespace when service namespace is empty",
			service: &admissionregistrationv1.ServiceReference{
				Name: "capsule-webhook-service",
			},
			defaultNamespace: "capsule-system",
			expected: []string{
				"capsule-webhook-service",
				"capsule-webhook-service.capsule-system",
				"capsule-webhook-service.capsule-system.svc",
				"capsule-webhook-service.capsule-system.svc.cluster.local",
			},
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var sans CertificateSANs

			sans.AddServiceReference(tt.service, tt.defaultNamespace)

			if !reflect.DeepEqual(tt.expected, sans.DNSNames) {
				t.Fatalf("expected DNS names %v, got %v", tt.expected, sans.DNSNames)
			}
		})
	}
}

func TestCertificateSANsAddURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		rawURL           *string
		expectedDNSNames []string
		expectedIPAddrs  []net.IP
		wantErr          bool
	}{
		{
			name: "nil URL is ignored",
		},
		{
			name:   "blank URL is ignored",
			rawURL: ptr.To("   "),
		},
		{
			name:             "adds dns hostname",
			rawURL:           ptr.To("https://webhook.example.com/mutate"),
			expectedDNSNames: []string{"webhook.example.com"},
		},
		{
			name:             "adds dns hostname without port",
			rawURL:           ptr.To("https://webhook.example.com:9443/mutate"),
			expectedDNSNames: []string{"webhook.example.com"},
		},
		{
			name:            "adds ipv4 address",
			rawURL:          ptr.To("https://10.96.0.10:9443/mutate"),
			expectedIPAddrs: []net.IP{net.ParseIP("10.96.0.10")},
		},
		{
			name:            "adds ipv6 address",
			rawURL:          ptr.To("https://[fd00::1]:9443/mutate"),
			expectedIPAddrs: []net.IP{net.ParseIP("fd00::1")},
		},
		{
			name:    "rejects invalid URL",
			rawURL:  ptr.To("://bad-url"),
			wantErr: true,
		},
		{
			name:    "rejects URL without host",
			rawURL:  ptr.To("https:///mutate"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var sans CertificateSANs

			err := sans.AddURL(tt.rawURL)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}

				return
			}

			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}

			expected := CertificateSANs{
				DNSNames: tt.expectedDNSNames,
				IPAddrs:  tt.expectedIPAddrs,
			}

			assertCertificateSANsEqual(t, expected, sans)
		})
	}
}

func TestCertificateSANsMatchesCertificate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sans        CertificateSANs
		certificate *x509.Certificate
		want        bool
	}{
		{
			name: "nil certificate does not match",
			sans: CertificateSANs{
				DNSNames: []string{"webhook.capsule-system.svc"},
			},
			want: false,
		},
		{
			name: "matches normalized dns and ip sans",
			sans: CertificateSANs{
				DNSNames: []string{
					" Webhook.Capsule-System.Svc ",
					"api.example.com",
				},
				IPAddrs: []net.IP{
					net.ParseIP("10.96.0.10"),
				},
			},
			certificate: &x509.Certificate{
				DNSNames: []string{
					"api.example.com",
					"webhook.capsule-system.svc",
				},
				IPAddresses: []net.IP{
					net.ParseIP("10.96.0.10"),
				},
			},
			want: true,
		},
		{
			name: "does not match when certificate misses dns san",
			sans: CertificateSANs{
				DNSNames: []string{
					"api.example.com",
					"webhook.capsule-system.svc",
				},
			},
			certificate: &x509.Certificate{
				DNSNames: []string{
					"webhook.capsule-system.svc",
				},
			},
			want: false,
		},
		{
			name: "does not match when certificate has stale extra dns san",
			sans: CertificateSANs{
				DNSNames: []string{
					"webhook.capsule-system.svc",
				},
			},
			certificate: &x509.Certificate{
				DNSNames: []string{
					"old-webhook.capsule-system.svc",
					"webhook.capsule-system.svc",
				},
			},
			want: false,
		},
		{
			name: "does not match when certificate misses ip san",
			sans: CertificateSANs{
				IPAddrs: []net.IP{
					net.ParseIP("10.96.0.10"),
				},
			},
			certificate: &x509.Certificate{},
			want:        false,
		},
		{
			name: "does not match when certificate has stale extra ip san",
			sans: CertificateSANs{
				IPAddrs: []net.IP{
					net.ParseIP("10.96.0.10"),
				},
			},
			certificate: &x509.Certificate{
				IPAddresses: []net.IP{
					net.ParseIP("10.96.0.10"),
					net.ParseIP("10.96.0.20"),
				},
			},
			want: false,
		},
		{
			name:        "matches empty sans against certificate without sans",
			sans:        CertificateSANs{},
			certificate: &x509.Certificate{},
			want:        true,
		},
	}

	for _, tt := range tests {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.sans.MatchesCertificate(tt.certificate); got != tt.want {
				t.Fatalf("expected MatchesCertificate() to be %t, got %t", tt.want, got)
			}
		})
	}
}

func assertCertificateSANsEqual(t *testing.T, expected, actual CertificateSANs) {
	t.Helper()

	expected = expected.Normalize()
	actual = actual.Normalize()

	if !reflect.DeepEqual(expected.DNSNames, actual.DNSNames) {
		t.Fatalf("expected DNS names %v, got %v", expected.DNSNames, actual.DNSNames)
	}

	expectedIPs := IPsToStrings(expected.IPAddrs)
	actualIPs := IPsToStrings(actual.IPAddrs)

	if !reflect.DeepEqual(expectedIPs, actualIPs) {
		t.Fatalf("expected IP addresses %v, got %v", expectedIPs, actualIPs)
	}
}
