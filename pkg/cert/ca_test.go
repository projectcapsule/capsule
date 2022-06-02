// Copyright 2020-2021 Clastix Labs
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
	var ca *CapsuleCA

	var err error

	ca, err = GenerateCertificateAuthority()
	assert.Nil(t, err)

	var crt *bytes.Buffer
	crt, err = ca.CACertificatePem()
	assert.Nil(t, err)

	var key *bytes.Buffer
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
			var ca *CapsuleCA
			var err error

			e := time.Now().AddDate(1, 0, 0)

			ca, err = GenerateCertificateAuthority()
			assert.Nil(t, err)

			var crt *bytes.Buffer
			var key *bytes.Buffer
			crt, key, err = ca.GenerateCertificate(NewCertOpts(e, c.dnsNames...))
			assert.Nil(t, err)

			var b *pem.Block
			var c *x509.Certificate
			b, _ = pem.Decode(crt.Bytes())
			c, err = x509.ParseCertificate(b.Bytes)
			assert.Nil(t, err)

			assert.Equal(t, e.Unix(), c.NotAfter.Unix())

			for _, i := range c.DNSNames {
				assert.Contains(t, c.DNSNames, i)
			}

			_, err = tls.X509KeyPair(crt.Bytes(), key.Bytes())
			assert.Nil(t, err)
		})
	}
}
