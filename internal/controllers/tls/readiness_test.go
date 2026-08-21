// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"context"
	cryptotls "crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	runtimeadmission "github.com/projectcapsule/capsule/pkg/runtime/admission"
)

func TestWebhookCertificateReadinessCheckAcceptsPublishedCA(t *testing.T) {
	t.Parallel()

	caBundle, certificatePEM, keyPEM := generateTestTLSMaterial(t, testWebhookSANs())
	validating := testValidatingWebhookConfiguration(caBundle)
	reconciler, kubeClient := newTestTLSReconciler(
		t,
		testTLSSecret(caBundle, certificatePEM, keyPEM),
		validating,
	)
	enableTestValidatingWebhook(t, kubeClient)
	servingCertificate := parseTestServingCertificate(t, certificatePEM, keyPEM)
	check := WebhookCertificateReadinessCheck(
		kubeClient,
		reconciler.Configuration,
		testNamespace,
		func(*cryptotls.ClientHelloInfo) (*cryptotls.Certificate, error) {
			return &servingCertificate, nil
		},
	)

	if err := check(httptest.NewRequest("GET", "/readyz", nil)); err != nil {
		t.Fatalf("readiness check rejected matching serving certificate and caBundle: %v", err)
	}
}

func TestWebhookCertificateReadinessCheckRejectsMismatchedPublishedCA(t *testing.T) {
	t.Parallel()

	caBundle, certificatePEM, keyPEM := generateTestTLSMaterial(t, testWebhookSANs())
	wrongCABundle, _, _ := generateTestTLSMaterial(t, testWebhookSANs())
	validating := testValidatingWebhookConfiguration(wrongCABundle)
	reconciler, kubeClient := newTestTLSReconciler(
		t,
		testTLSSecret(caBundle, certificatePEM, keyPEM),
		validating,
	)
	enableTestValidatingWebhook(t, kubeClient)
	servingCertificate := parseTestServingCertificate(t, certificatePEM, keyPEM)
	check := WebhookCertificateReadinessCheck(
		kubeClient,
		reconciler.Configuration,
		testNamespace,
		func(*cryptotls.ClientHelloInfo) (*cryptotls.Certificate, error) {
			return &servingCertificate, nil
		},
	)

	err := check(httptest.NewRequest("GET", "/readyz", nil))
	if err == nil {
		t.Fatal("readiness check accepted a caBundle that does not trust the serving certificate")
	}
	if !strings.Contains(err.Error(), "does not trust the serving certificate") {
		t.Fatalf("readiness error = %v, want certificate trust failure", err)
	}
}

func TestWebhookCertificateReadinessCheckRejectsStaleLoadedCertificate(t *testing.T) {
	t.Parallel()

	caBundle, persistedCertificate, persistedKey := generateTestTLSMaterial(t, testWebhookSANs())
	_, staleCertificate, staleKey := generateTestTLSMaterial(t, testWebhookSANs())
	reconciler, kubeClient := newTestTLSReconciler(
		t,
		testTLSSecret(caBundle, persistedCertificate, persistedKey),
	)
	loadedCertificate := parseTestServingCertificate(t, staleCertificate, staleKey)
	check := WebhookCertificateReadinessCheck(
		kubeClient,
		reconciler.Configuration,
		testNamespace,
		func(*cryptotls.ClientHelloInfo) (*cryptotls.Certificate, error) {
			return &loadedCertificate, nil
		},
	)

	err := check(httptest.NewRequest("GET", "/readyz", nil))
	if err == nil {
		t.Fatal("readiness check accepted a stale certificate loaded by certwatcher")
	}
	if !strings.Contains(err.Error(), "is stale relative to TLS Secret") {
		t.Fatalf("readiness error = %v, want stale loaded certificate failure", err)
	}
}

func TestWebhookCertificateReadinessCheckRequiresManagedConfiguration(t *testing.T) {
	t.Parallel()

	caBundle, certificatePEM, keyPEM := generateTestTLSMaterial(t, testWebhookSANs())
	reconciler, kubeClient := newTestTLSReconciler(t, testTLSSecret(caBundle, certificatePEM, keyPEM))
	enableTestValidatingWebhook(t, kubeClient)
	servingCertificate := parseTestServingCertificate(t, certificatePEM, keyPEM)
	check := WebhookCertificateReadinessCheck(
		kubeClient,
		reconciler.Configuration,
		testNamespace,
		func(*cryptotls.ClientHelloInfo) (*cryptotls.Certificate, error) {
			return &servingCertificate, nil
		},
	)

	err := check(httptest.NewRequest("GET", "/readyz", nil))
	if err == nil {
		t.Fatal("readiness check accepted a missing managed webhook configuration")
	}
	if !strings.Contains(err.Error(), "get validating webhook configuration") {
		t.Fatalf("readiness error = %v, want missing configuration failure", err)
	}
}

func testValidatingWebhookConfiguration(caBundle []byte) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: testValidatingConfiguration},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name: "owners.validating.projectcapsule.dev",
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				CABundle: caBundle,
			},
		}},
	}
}

func enableTestValidatingWebhook(t *testing.T, kubeClient client.Client) {
	t.Helper()

	configurationObject := &capsulev1beta2.CapsuleConfiguration{}
	if err := kubeClient.Get(
		context.Background(),
		client.ObjectKey{Name: "capsule"},
		configurationObject,
	); err != nil {
		t.Fatalf("get CapsuleConfiguration: %v", err)
	}

	configurationObject.Spec.Admission.Validating.Webhooks = []*runtimeadmission.ValidatingWebhook{{}}
	if err := kubeClient.Update(context.Background(), configurationObject); err != nil {
		t.Fatalf("update CapsuleConfiguration: %v", err)
	}
}

func parseTestServingCertificate(t *testing.T, certificatePEM, keyPEM []byte) cryptotls.Certificate {
	t.Helper()

	servingCertificate, err := cryptotls.X509KeyPair(certificatePEM, keyPEM)
	if err != nil {
		t.Fatalf("parse test serving certificate: %v", err)
	}

	return servingCertificate
}
