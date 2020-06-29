package cert

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

type Ca interface {
	GenerateCertificate(opts CertificateOptions) (certificatePem *bytes.Buffer, certificateKey *bytes.Buffer, err error)
	CaCertificatePem() (b *bytes.Buffer, err error)
	CaPrivateKeyPem() (b *bytes.Buffer, err error)
	ExpiresIn(now time.Time) (time.Duration, error)
}

type CapsuleCa struct {
	ca         *x509.Certificate
	privateKey *rsa.PrivateKey
}

func (c CapsuleCa) isAlreadyValid(now time.Time) bool {
	return now.After(c.ca.NotBefore)
}

func (c CapsuleCa) isExpired(now time.Time) bool {
	return now.Before(c.ca.NotAfter)
}

func (c CapsuleCa) ExpiresIn(now time.Time) (time.Duration, error) {
	if !c.isExpired(now) {
		return time.Nanosecond, CaExpiredError{}
	}
	if !c.isAlreadyValid(now) {
		return time.Nanosecond, CaNotYetValidError{}
	}
	return time.Duration(c.ca.NotAfter.Unix() - now.Unix()) * time.Second, nil
}

func (c CapsuleCa) CaCertificatePem() (b *bytes.Buffer, err error) {
	var crtBytes []byte
	crtBytes, err = x509.CreateCertificate(rand.Reader, c.ca, c.ca, &c.privateKey.PublicKey, c.privateKey)
	if err != nil {
		return
	}
	b = new(bytes.Buffer)
	err = pem.Encode(b, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: crtBytes,
	})
	return b, err
}

func (c CapsuleCa) CaPrivateKeyPem() (b *bytes.Buffer, err error) {
	b = new(bytes.Buffer)
	return b, pem.Encode(b, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(c.privateKey),
	})
}

func GenerateCertificateAuthority() (s *CapsuleCa, err error) {
	s = &CapsuleCa{
		ca: &x509.Certificate{
			SerialNumber: big.NewInt(2019),
			Subject: pkix.Name{
				Organization:  []string{"Clastix"},
				Country:       []string{"UK"},
				Province:      []string{""},
				Locality:      []string{"London"},
				StreetAddress: []string{"27, Old Gloucester Street"},
				PostalCode:    []string{"WC1N 3AX"},
			},
			NotBefore:             time.Now(),
			NotAfter:              time.Now().AddDate(10, 0, 0),
			IsCA:                  true,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
			BasicConstraintsValid: true,
		},
	}

	s.privateKey, err = rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, err
	}

	return
}

func NewCertificateAuthorityFromBytes(certBytes, keyBytes []byte) (s *CapsuleCa, err error) {
	var b *pem.Block

	b, _ = pem.Decode(certBytes)
	var cert *x509.Certificate
	if cert, err = x509.ParseCertificate(b.Bytes); err != nil {
		return
	}

	b, _ = pem.Decode(keyBytes)
	var key *rsa.PrivateKey
	if key, err = x509.ParsePKCS1PrivateKey(b.Bytes); err != nil {
		return
	}

	s = &CapsuleCa{
		ca: cert,
		privateKey: key,
	}

	return
}

func (c *CapsuleCa) GenerateCertificate(opts CertificateOptions) (certificatePem *bytes.Buffer, certificateKey *bytes.Buffer, err error) {
	certPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, err
	}

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1658),
		Subject: pkix.Name{
			Organization:  []string{"Clastix"},
			Country:       []string{"UK"},
			Province:      []string{""},
			Locality:      []string{"London"},
			StreetAddress: []string{"27, Old Gloucester Street"},
			PostalCode:    []string{"WC1N 3AX"},
		},
		DNSNames:     opts.DnsNames(),
		NotBefore:    time.Now().AddDate(0, 0, -1),
		NotAfter:     opts.ExpirationDate(),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, cert, c.ca, &certPrivKey.PublicKey, c.privateKey)
	if err != nil {
		return nil, nil, err
	}

	certificatePem = new(bytes.Buffer)
	err = pem.Encode(certificatePem, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certBytes,
	})
	if err != nil {
		return
	}

	certificateKey = new(bytes.Buffer)
	err = pem.Encode(certificateKey, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrivKey),
	})
	if err != nil {
		return
	}

	return
}
