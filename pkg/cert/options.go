package cert

import "time"

type CertificateOptions interface {
	DnsNames() []string
	ExpirationDate() time.Time
}

type certOpts struct {
	dnsNames       []string
	expirationDate time.Time
}

func (c certOpts) DnsNames() []string {
	return c.dnsNames
}

func (c certOpts) ExpirationDate() time.Time {
	return c.expirationDate
}

func NewCertOpts(expirationDate time.Time, dnsNames ...string) *certOpts {
	return &certOpts{dnsNames: dnsNames, expirationDate: expirationDate}
}
