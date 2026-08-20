// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	apiMeta "github.com/projectcapsule/capsule/pkg/api/meta"
	runtimeadmission "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/cert"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

const (
	testNamespace               = "capsule-system"
	testSecretName              = "capsule-tls"
	testServiceName             = "capsule-webhook-service"
	testMutatingConfiguration   = "capsule-mutating-webhook-configuration"
	testValidatingConfiguration = "capsule-validating-webhook-configuration"
)

func TestReconcileCertificatesPreservesValidExternalTLSSecret(t *testing.T) {
	t.Parallel()

	externalCABundle, externalCertificate, externalKey := generateTestTLSMaterial(t, testWebhookSANs())
	secret := testTLSSecret(externalCABundle, externalCertificate, externalKey)
	reconciler, kubeClient := newTestTLSReconciler(t, secret)

	if err := reconciler.ReconcileCertificates(context.Background(), logr.Discard(), secret.DeepCopy()); err != nil {
		t.Fatalf("ReconcileCertificates() error = %v", err)
	}

	updated := getTestTLSSecret(t, kubeClient)
	if len(updated.Data["ca.key"]) != 0 {
		t.Fatal("external TLS Secret unexpectedly gained a CA private key")
	}

	assertTLSDataEqual(t, updated, externalCABundle, externalCertificate, externalKey)
}

func TestReconcileCertificatesRejectsUnsafeExternalCARotation(t *testing.T) {
	t.Parallel()

	externalCABundle, externalCertificate, externalKey := generateTestTLSMaterial(t, cert.CertificateSANs{
		DNSNames: []string{testServiceName + "." + testNamespace + ".svc"},
	})
	secret := testTLSSecret(externalCABundle, externalCertificate, externalKey)
	reconciler, kubeClient := newTestTLSReconciler(t, secret)

	err := reconciler.ReconcileCertificates(context.Background(), logr.Discard(), secret.DeepCopy())
	if err == nil {
		t.Fatal("ReconcileCertificates() unexpectedly replaced externally managed CA material")
	}
	if !strings.Contains(err.Error(), "refusing automatic CA replacement") {
		t.Fatalf("ReconcileCertificates() error = %v, want safe CA replacement refusal", err)
	}

	updated := getTestTLSSecret(t, kubeClient)
	if len(updated.Data["ca.key"]) != 0 {
		t.Fatal("rejected external TLS Secret unexpectedly gained a CA private key")
	}

	assertTLSDataEqual(t, updated, externalCABundle, externalCertificate, externalKey)
}

func TestReconcileCertificatesConcurrentReplicasAdoptPersistedWinner(t *testing.T) {
	t.Parallel()

	secret := testTLSSecret(nil, nil, nil)
	reconciler, kubeClient := newTestTLSReconciler(t, secret)
	firstReplica := secret.DeepCopy()
	secondReplica := secret.DeepCopy()
	start := make(chan struct{})
	errors := make(chan error, 2)

	for _, replica := range []*corev1.Secret{firstReplica, secondReplica} {
		go func(replica *corev1.Secret) {
			<-start
			errors <- reconciler.ReconcileCertificates(context.Background(), logr.Discard(), replica)
		}(replica)
	}

	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("concurrent ReconcileCertificates() error = %v", err)
		}
	}

	finalPersisted := getTestTLSSecret(t, kubeClient)
	if !bytes.Equal(
		firstReplica.Data[corev1.ServiceAccountRootCAKey],
		finalPersisted.Data[corev1.ServiceAccountRootCAKey],
	) {
		t.Fatal("first replica did not adopt the persisted CA")
	}
	if !bytes.Equal(
		secondReplica.Data[corev1.ServiceAccountRootCAKey],
		finalPersisted.Data[corev1.ServiceAccountRootCAKey],
	) {
		t.Fatal("second replica did not adopt the persisted CA")
	}
	if !bytes.Equal(firstReplica.Data[corev1.TLSCertKey], finalPersisted.Data[corev1.TLSCertKey]) ||
		!bytes.Equal(secondReplica.Data[corev1.TLSCertKey], finalPersisted.Data[corev1.TLSCertKey]) {
		t.Fatal("concurrent replicas did not adopt the persisted serving certificate")
	}
}

func TestReconcileCertificatesPopulatesChartCreatedOpaqueSecret(t *testing.T) {
	t.Parallel()

	// The Helm chart must create this empty Secret before the controller Pod can
	// mount it. Kubernetes does not allow changing a Secret's type afterwards,
	// so reconciliation must preserve Opaque while adding valid TLS material.
	secret := testTLSSecret(nil, nil, nil)
	secret.Type = corev1.SecretTypeOpaque
	reconciler, kubeClient := newTestTLSReconciler(t, secret)

	if err := reconciler.ReconcileCertificates(context.Background(), logr.Discard(), secret.DeepCopy()); err != nil {
		t.Fatalf("ReconcileCertificates() error = %v", err)
	}

	updated := getTestTLSSecret(t, kubeClient)
	if updated.Type != corev1.SecretTypeOpaque {
		t.Fatalf("reconciled Secret type = %q, want %q", updated.Type, corev1.SecretTypeOpaque)
	}
	if len(updated.Data[corev1.ServiceAccountRootCAKey]) == 0 ||
		len(updated.Data[corev1.TLSCertKey]) == 0 ||
		len(updated.Data[corev1.TLSPrivateKeyKey]) == 0 {
		t.Fatal("reconciled Secret is missing generated TLS material")
	}
}

func TestReconcileCertificatesPatchesEveryAdmissionCABundle(t *testing.T) {
	t.Parallel()

	oldCA, _, _ := generateTestTLSMaterial(t, testWebhookSANs())
	secret := testTLSSecret(nil, nil, nil)
	mutating := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: testMutatingConfiguration},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{Name: "first.mutating.projectcapsule.dev", ClientConfig: admissionregistrationv1.WebhookClientConfig{CABundle: oldCA}},
			{Name: "second.mutating.projectcapsule.dev"},
		},
	}
	validating := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: testValidatingConfiguration},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{Name: "first.validating.projectcapsule.dev", ClientConfig: admissionregistrationv1.WebhookClientConfig{CABundle: oldCA}},
			{Name: "second.validating.projectcapsule.dev"},
		},
	}
	reconciler, kubeClient := newTestTLSReconciler(t, secret, mutating, validating)

	if err := reconciler.ReconcileCertificates(context.Background(), logr.Discard(), secret.DeepCopy()); err != nil {
		t.Fatalf("ReconcileCertificates() error = %v", err)
	}

	wantCA := getTestTLSSecret(t, kubeClient).Data[corev1.ServiceAccountRootCAKey]
	updatedMutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := kubeClient.Get(
		context.Background(),
		client.ObjectKey{Name: testMutatingConfiguration},
		updatedMutating,
	); err != nil {
		t.Fatalf("get mutating webhook configuration: %v", err)
	}
	for index := range updatedMutating.Webhooks {
		if !bytes.Equal(updatedMutating.Webhooks[index].ClientConfig.CABundle, wantCA) {
			t.Fatalf("mutating webhook %q has stale caBundle", updatedMutating.Webhooks[index].Name)
		}
	}

	updatedValidating := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := kubeClient.Get(
		context.Background(),
		client.ObjectKey{Name: testValidatingConfiguration},
		updatedValidating,
	); err != nil {
		t.Fatalf("get validating webhook configuration: %v", err)
	}
	for index := range updatedValidating.Webhooks {
		if !bytes.Equal(updatedValidating.Webhooks[index].ClientConfig.CABundle, wantCA) {
			t.Fatalf("validating webhook %q has stale caBundle", updatedValidating.Webhooks[index].Name)
		}
	}
}

func newTestTLSReconciler(t *testing.T, objects ...client.Object) (*Reconciler, client.Client) {
	t.Helper()

	configurationObject := &capsulev1beta2.CapsuleConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "capsule"},
		Spec: capsulev1beta2.CapsuleConfigurationSpec{
			EnableTLSReconciler: true,
			CapsuleResources: capsulev1beta2.CapsuleResources{
				TLSSecretName: testSecretName,
			},
			Admission: capsulev1beta2.DynamicAdmission{
				ServiceName: testServiceName,
				Mutating: &capsulev1beta2.DynamicMutatingAdmissionConfig{
					DynamicAdmissionConfig: runtimeadmission.DynamicAdmissionConfig{
						Name: apiMeta.RFC1123Name(testMutatingConfiguration),
					},
				},
				Validating: &capsulev1beta2.DynamicValidatingAdmissionConfig{
					DynamicAdmissionConfig: runtimeadmission.DynamicAdmissionConfig{
						Name: apiMeta.RFC1123Name(testValidatingConfiguration),
					},
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	for _, addToScheme := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		admissionregistrationv1.AddToScheme,
		apiextensionsv1.AddToScheme,
		capsulev1beta2.AddToScheme,
	} {
		if err := addToScheme(scheme); err != nil {
			t.Fatal(err)
		}
	}

	objects = append(objects, configurationObject)
	kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	cfg := configuration.NewCapsuleConfiguration(
		context.Background(),
		kubeClient,
		kubeClient,
		nil,
		configurationObject.Name,
	)

	return &Reconciler{
		Client:        kubeClient,
		Log:           logr.Discard(),
		Namespace:     testNamespace,
		Configuration: cfg,
	}, kubeClient
}

func testTLSSecret(caBundle, certificate, key []byte) *corev1.Secret {
	data := map[string][]byte{}
	if len(caBundle) > 0 {
		data[corev1.ServiceAccountRootCAKey] = caBundle
	}
	if len(certificate) > 0 {
		data[corev1.TLSCertKey] = certificate
	}
	if len(key) > 0 {
		data[corev1.TLSPrivateKeyKey] = key
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Type:       corev1.SecretTypeTLS,
		Data:       data,
	}
}

func getTestTLSSecret(t *testing.T, kubeClient client.Client) *corev1.Secret {
	t.Helper()

	secret := &corev1.Secret{}
	if err := kubeClient.Get(
		context.Background(),
		client.ObjectKey{Namespace: testNamespace, Name: testSecretName},
		secret,
	); err != nil {
		t.Fatalf("get reconciled TLS Secret: %v", err)
	}

	return secret
}

func assertTLSDataEqual(t *testing.T, secret *corev1.Secret, caBundle, certificate, key []byte) {
	t.Helper()

	if !bytes.Equal(secret.Data[corev1.ServiceAccountRootCAKey], caBundle) {
		t.Fatal("TLS Secret CA bundle changed")
	}
	if !bytes.Equal(secret.Data[corev1.TLSCertKey], certificate) {
		t.Fatal("TLS Secret serving certificate changed")
	}
	if !bytes.Equal(secret.Data[corev1.TLSPrivateKeyKey], key) {
		t.Fatal("TLS Secret serving private key changed")
	}
}

func testWebhookSANs() cert.CertificateSANs {
	return cert.CertificateSANs{DNSNames: []string{
		testServiceName,
		testServiceName + "." + testNamespace,
		testServiceName + "." + testNamespace + ".svc",
		testServiceName + "." + testNamespace + ".svc.cluster.local",
	}}
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
