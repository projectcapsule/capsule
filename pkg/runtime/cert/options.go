// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package cert

import "time"

type CertificateOptions interface {
	GetDNSNames() CertificateSANs
	GetExpirationDate() time.Time
}

type CertOpts struct {
	SAN            CertificateSANs
	ExpirationDate time.Time
}

func NewCertOpts(expirationDate time.Time, sans CertificateSANs) CertOpts {
	return CertOpts{
		ExpirationDate: expirationDate,
		SAN:            sans.Normalize(),
	}
}

func (c CertOpts) GetDNSNames() CertificateSANs {
	return c.SAN
}

func (c CertOpts) GetExpirationDate() time.Time {
	return c.ExpirationDate
}
