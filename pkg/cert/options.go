// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package cert

import "time"

type CertificateOptions interface {
	DNSNames() []string
	ExpirationDate() time.Time
}

type certOpts struct {
	dnsNames       []string
	expirationDate time.Time
}

func (c certOpts) DNSNames() []string {
	return c.dnsNames
}

func (c certOpts) ExpirationDate() time.Time {
	return c.expirationDate
}

func NewCertOpts(expirationDate time.Time, dnsNames ...string) CertificateOptions {
	return &certOpts{dnsNames: dnsNames, expirationDate: expirationDate}
}
