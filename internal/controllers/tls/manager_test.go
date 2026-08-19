// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/runtime/cert"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

func TestReconcileCertificatesMigratesExternalTLSSecret(t *testing.T) {
	t.Parallel()

	const (
		namespace   = "capsule-system"
		secretName  = "capsule-tls"
		serviceName = "capsule-webhook-service"
	)

	ctx := context.Background()
	externalCABundle, externalCertificate, externalKey := generateTestTLSMaterial(t, cert.CertificateSANs{
		DNSNames: []string{serviceName + "." + namespace + ".svc"},
	})

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.ServiceAccountRootCAKey: externalCABundle,
			corev1.TLSCertKey:              externalCertificate,
			corev1.TLSPrivateKeyKey:        externalKey,
		},
	}

	configurationObject := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			EnableTLSReconciler: true,
			CapsuleResources: capsulev1beta2.CapsuleResources{
				TLSSecretName: secretName,
			},
			Admission: capsulev1beta2.DynamicAdmission{
				ServiceName: serviceName,
				Mutating:    &capsulev1beta2.DynamicMutatingAdmissionConfig{},
				Validating:  &capsulev1beta2.DynamicValidatingAdmissionConfig{},
			},
		},
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	if err := capsulev1beta2.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	kubeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, configurationObject).
		Build()

	reconciler := &Reconciler{
		Client:    kubeClient,
		Log:       logr.Discard(),
		Namespace: namespace,
		Configuration: configuration.NewCapsuleConfiguration(
			ctx,
			kubeClient,
			kubeClient,
			nil,
			configurationObject.Name,
		),
	}

	desiredSANs, err := reconciler.desiredWebhookSANs(ctx)
	if err != nil {
		t.Fatalf("desiredWebhookSANs() error = %v", err)
	}

	if err := reconciler.validateSecretCertificate(secret, desiredSANs); err == nil {
		t.Fatal("external serving certificate unexpectedly satisfies all desired SANs")
	}

	if err := reconciler.ReconcileCertificates(ctx, logr.Discard(), secret.DeepCopy()); err != nil {
		t.Fatalf("ReconcileCertificates() error = %v", err)
	}

	updated := &corev1.Secret{}
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(secret), updated); err != nil {
		t.Fatalf("get reconciled TLS Secret: %v", err)
	}

	if len(updated.Data["ca.key"]) == 0 {
		t.Fatal("reconciled TLS Secret does not contain ca.key")
	}

	if bytes.Equal(updated.Data[corev1.ServiceAccountRootCAKey], externalCABundle) {
		t.Fatal("reconciled TLS Secret retained the external CA bundle")
	}

	if bytes.Equal(updated.Data[corev1.TLSCertKey], externalCertificate) {
		t.Fatal("reconciled TLS Secret retained the external serving certificate")
	}

	if _, err := cert.NewCertificateAuthorityFromBytes(
		updated.Data[corev1.ServiceAccountRootCAKey],
		updated.Data["ca.key"],
	); err != nil {
		t.Fatalf("reconciled CA certificate/key pair is invalid: %v", err)
	}

	if err := reconciler.validateSecretCertificate(updated, desiredSANs); err != nil {
		t.Fatalf("reconciled serving certificate is invalid: %v", err)
	}
}

func generateTestTLSMaterial(
	t *testing.T,
	sans cert.CertificateSANs,
) ([]byte, []byte, []byte) {
	t.Helper()

	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		t.Fatalf("generate test CA: %v", err)
	}

	caBundle, err := ca.CACertificatePem()
	if err != nil {
		t.Fatalf("encode test CA: %v", err)
	}

	certificate, key, err := ca.GenerateCertificate(cert.NewCertOpts(
		time.Now().Add(certificateValidity),
		sans,
	))
	if err != nil {
		t.Fatalf("generate test serving certificate: %v", err)
	}

	return caBundle.Bytes(), certificate.Bytes(), key.Bytes()
}
