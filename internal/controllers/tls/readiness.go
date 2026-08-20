// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type certificateGetter func(*tls.ClientHelloInfo) (*tls.Certificate, error)

// WebhookCertificateReadinessCheck prevents a webhook Pod from becoming ready
// while the certificate it is actually serving is not trusted by an admission
// configuration managed by Capsule. This closes the window between Secret
// publication, certwatcher reload, and caBundle reconciliation.
func WebhookCertificateReadinessCheck(
	reader client.Reader,
	cfg configuration.Configuration,
	namespace string,
	getCertificate certificateGetter,
) healthz.Checker {
	return func(request *http.Request) error {
		servingCertificate, err := getCertificate(nil)
		if err != nil {
			return fmt.Errorf("load webhook serving certificate: %w", err)
		}

		if servingCertificate == nil || len(servingCertificate.Certificate) == 0 {
			return fmt.Errorf("webhook serving certificate is empty")
		}

		leaf, err := x509.ParseCertificate(servingCertificate.Certificate[0])
		if err != nil {
			return fmt.Errorf("parse webhook serving certificate: %w", err)
		}

		secret := &corev1.Secret{}

		secretKey := types.NamespacedName{Namespace: namespace, Name: cfg.TLSSecretName()}
		if err := reader.Get(request.Context(), secretKey, secret); err != nil {
			return fmt.Errorf("get webhook TLS Secret %s: %w", secretKey.String(), err)
		}

		persistedLeaf, err := certificateFromPEM(secret.Data[corev1.TLSCertKey])
		if err != nil {
			return fmt.Errorf("parse serving certificate in TLS Secret %s: %w", secretKey.String(), err)
		}

		if !bytes.Equal(leaf.Raw, persistedLeaf.Raw) {
			return fmt.Errorf("loaded webhook serving certificate is stale relative to TLS Secret %s", secretKey.String())
		}

		admission := cfg.Admission()
		trusts := make([]admissionWebhookTrust, 0)

		if admission.Mutating != nil && len(admission.Mutating.Webhooks) > 0 {
			loaded, err := loadAdmissionWebhookTrust(
				request.Context(),
				reader,
				"mutating",
				string(admission.Mutating.Name),
				func() client.Object { return &admissionregistrationv1.MutatingWebhookConfiguration{} },
				mutatingWebhookTrust,
			)
			if err != nil {
				return err
			}

			trusts = append(trusts, loaded...)
		}

		if admission.Validating != nil && len(admission.Validating.Webhooks) > 0 {
			loaded, err := loadAdmissionWebhookTrust(
				request.Context(),
				reader,
				"validating",
				string(admission.Validating.Name),
				func() client.Object { return &admissionregistrationv1.ValidatingWebhookConfiguration{} },
				validatingWebhookTrust,
			)
			if err != nil {
				return err
			}

			trusts = append(trusts, loaded...)
		}

		for _, trust := range trusts {
			if err := verifyCertificateAgainstCABundle(leaf, trust.caBundle); err != nil {
				return fmt.Errorf(
					"%s webhook configuration %q webhook %q does not trust the serving certificate: %w",
					trust.kind,
					trust.configurationName,
					trust.webhookName,
					err,
				)
			}
		}

		return nil
	}
}

func certificateFromPEM(certificatePEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		return nil, fmt.Errorf("invalid certificate PEM")
	}

	return x509.ParseCertificate(block.Bytes)
}

type admissionWebhookTrust struct {
	kind              string
	configurationName string
	webhookName       string
	caBundle          []byte
}

func loadAdmissionWebhookTrust(
	ctx context.Context,
	reader client.Reader,
	kind string,
	name string,
	newConfiguration func() client.Object,
	extractTrust func(client.Object, string, string) ([]admissionWebhookTrust, error),
) ([]admissionWebhookTrust, error) {
	if name == "" {
		return nil, fmt.Errorf("%s webhook configuration name is empty", kind)
	}

	webhookConfiguration := newConfiguration()
	if err := reader.Get(ctx, types.NamespacedName{Name: name}, webhookConfiguration); err != nil {
		return nil, fmt.Errorf("get %s webhook configuration %q: %w", kind, name, err)
	}

	trusts, err := extractTrust(webhookConfiguration, kind, name)
	if err != nil {
		return nil, err
	}

	if len(trusts) == 0 {
		return nil, fmt.Errorf("%s webhook configuration %q contains no webhooks", kind, name)
	}

	return trusts, nil
}

func mutatingWebhookTrust(
	object client.Object,
	kind string,
	configurationName string,
) ([]admissionWebhookTrust, error) {
	configuration, ok := object.(*admissionregistrationv1.MutatingWebhookConfiguration)
	if !ok {
		return nil, fmt.Errorf("expected MutatingWebhookConfiguration, got %T", object)
	}

	trusts := make([]admissionWebhookTrust, 0, len(configuration.Webhooks))
	for index := range configuration.Webhooks {
		trusts = append(trusts, admissionWebhookTrust{
			kind:              kind,
			configurationName: configurationName,
			webhookName:       configuration.Webhooks[index].Name,
			caBundle:          configuration.Webhooks[index].ClientConfig.CABundle,
		})
	}

	return trusts, nil
}

func validatingWebhookTrust(
	object client.Object,
	kind string,
	configurationName string,
) ([]admissionWebhookTrust, error) {
	configuration, ok := object.(*admissionregistrationv1.ValidatingWebhookConfiguration)
	if !ok {
		return nil, fmt.Errorf("expected ValidatingWebhookConfiguration, got %T", object)
	}

	trusts := make([]admissionWebhookTrust, 0, len(configuration.Webhooks))
	for index := range configuration.Webhooks {
		trusts = append(trusts, admissionWebhookTrust{
			kind:              kind,
			configurationName: configurationName,
			webhookName:       configuration.Webhooks[index].Name,
			caBundle:          configuration.Webhooks[index].ClientConfig.CABundle,
		})
	}

	return trusts, nil
}

func verifyCertificateAgainstCABundle(leaf *x509.Certificate, caBundle []byte) error {
	if len(caBundle) == 0 {
		return fmt.Errorf("caBundle is empty")
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caBundle) {
		return fmt.Errorf("caBundle is not valid PEM certificate data")
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots: roots,
		KeyUsages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}); err != nil {
		return err
	}

	return nil
}
