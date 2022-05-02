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

func TestCapsuleCa_IsValid(t *testing.T) {
	type testCase struct {
		notBefore   time.Time
		notAfter    time.Time
		returnError bool
	}

	tc := map[string]testCase{
		"ok":       {time.Now().AddDate(0, 0, -1), time.Now().AddDate(0, 0, 1), false},
		"expired":  {time.Now().AddDate(1, 0, 0), time.Now(), true},
		"notValid": {time.Now().AddDate(0, 0, 1), time.Now().AddDate(0, 0, 2), true},
	}

	for name, c := range tc {
		t.Run(name, func(t *testing.T) {
			var ca *CapsuleCA
			var err error

			ca, err = GenerateCertificateAuthority()
			assert.Nil(t, err)

			ca.certificate.NotAfter = c.notAfter
			ca.certificate.NotBefore = c.notBefore

			var w time.Duration
			w, err = ca.ExpiresIn(time.Now())
			if c.returnError {
				assert.Error(t, err)

				return
			}
			assert.Nil(t, err)
			assert.WithinDuration(t, ca.certificate.NotAfter, time.Now().Add(w), time.Minute)
		})
	}
}
