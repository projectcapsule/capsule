// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package cert

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewCertificateAuthorityFromBytes(t *testing.T) {
	var (
		ca  *CapsuleCA
		crt *bytes.Buffer
		key *bytes.Buffer
		err error
	)

	ca, err = GenerateCertificateAuthority()
	assert.Nil(t, err)

	crt, err = ca.CACertificatePem()
	assert.Nil(t, err)

	key, err = ca.CAPrivateKeyPem()
	assert.Nil(t, err)

	_, err = NewCertificateAuthorityFromBytes(crt.Bytes(), key.Bytes())
	assert.Nil(t, err)
}

func TestCapsuleCa_GenerateCertificate(t *testing.T) {
	type testCase struct {
		dnsNames []string
	}

	for name, c := range map[string]testCase{
		"foo.tld": {[]string{"foo.tld"}},
		"SAN":     {[]string{"capsule-webhook-service.capsule-system.svc", "capsule-webhook-service.capsule-system.default.cluster"}},
	} {
		t.Run(name, func(t *testing.T) {
			var (
				ca   *CapsuleCA
				crt  *bytes.Buffer
				key  *bytes.Buffer
				b    *pem.Block
				cert *x509.Certificate
				err  error
			)

			e := time.Now().AddDate(1, 0, 0)

			ca, err = GenerateCertificateAuthority()
			assert.Nil(t, err)

			crt, key, err = ca.GenerateCertificate(NewCertOpts(e, c.dnsNames...))
			assert.Nil(t, err)

			b, _ = pem.Decode(crt.Bytes())
			cert, err = x509.ParseCertificate(b.Bytes)
			assert.Nil(t, err)

			assert.Equal(t, e.Unix(), cert.NotAfter.Unix())

			for _, i := range c.DNSNames {
				assert.Contains(t, c.DNSNames, i)
			}

			_, err = tls.X509KeyPair(crt.Bytes(), key.Bytes())
			assert.Nil(t, err)
		})
	}
}
