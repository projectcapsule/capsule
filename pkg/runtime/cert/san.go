// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

type CertificateSANs struct {
	DNSNames []string
	IPAddrs  []net.IP
}

func (s CertificateSANs) Empty() bool {
	return len(s.DNSNames) == 0 && len(s.IPAddrs) == 0
}

func (s CertificateSANs) Normalize() CertificateSANs {
	dnsSet := make(map[string]struct{}, len(s.DNSNames))
	dnsNames := make([]string, 0, len(s.DNSNames))

	for _, name := range s.DNSNames {
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			continue
		}

		if _, ok := dnsSet[name]; ok {
			continue
		}

		dnsSet[name] = struct{}{}
		dnsNames = append(dnsNames, name)
	}

	ipSet := make(map[string]struct{}, len(s.IPAddrs))
	ipAddrs := make([]net.IP, 0, len(s.IPAddrs))

	for _, ip := range s.IPAddrs {
		if ip == nil {
			continue
		}

		normalized := ip.String()
		if _, ok := ipSet[normalized]; ok {
			continue
		}

		ipSet[normalized] = struct{}{}
		ipAddrs = append(ipAddrs, ip)
	}

	sort.Strings(dnsNames)
	sort.Slice(ipAddrs, func(i, j int) bool {
		return ipAddrs[i].String() < ipAddrs[j].String()
	})

	return CertificateSANs{
		DNSNames: dnsNames,
		IPAddrs:  ipAddrs,
	}
}

func (s *CertificateSANs) AddDNSNames(names ...string) {
	s.DNSNames = append(s.DNSNames, names...)
}

func (s *CertificateSANs) AddIPAddrs(ips ...net.IP) {
	s.IPAddrs = append(s.IPAddrs, ips...)
}

func (s *CertificateSANs) AddServiceReference(
	service *admissionregistrationv1.ServiceReference,
	defaultNamespace string,
) {
	if service == nil || service.Name == "" {
		return
	}

	namespace := service.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	s.AddDNSNames(
		service.Name,
		fmt.Sprintf("%s.%s", service.Name, namespace),
		fmt.Sprintf("%s.%s.svc", service.Name, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", service.Name, namespace),
	)
}

func (s *CertificateSANs) AddURL(rawURL *string) error {
	if rawURL == nil || strings.TrimSpace(*rawURL) == "" {
		return nil
	}

	parsed, err := url.Parse(*rawURL)
	if err != nil {
		return fmt.Errorf("parse webhook URL %q: %w", *rawURL, err)
	}

	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("webhook URL %q has empty host", *rawURL)
	}

	if ip := net.ParseIP(host); ip != nil {
		s.AddIPAddrs(ip)

		return nil
	}

	s.AddDNSNames(host)

	return nil
}

func (s CertificateSANs) CoveredByCertificate(certificate *x509.Certificate) bool {
	desired := s.Normalize()

	actualDNS := make(map[string]struct{}, len(certificate.DNSNames))
	for _, name := range certificate.DNSNames {
		name = strings.TrimSpace(strings.ToLower(name))
		if name == "" {
			continue
		}

		actualDNS[name] = struct{}{}
	}

	for _, name := range desired.DNSNames {
		if _, ok := actualDNS[name]; !ok {
			return false
		}
	}

	actualIPs := make(map[string]struct{}, len(certificate.IPAddresses))
	for _, ip := range certificate.IPAddresses {
		if ip == nil {
			continue
		}

		actualIPs[ip.String()] = struct{}{}
	}

	for _, ip := range desired.IPAddrs {
		if ip == nil {
			continue
		}

		if _, ok := actualIPs[ip.String()]; !ok {
			return false
		}
	}

	return true
}
