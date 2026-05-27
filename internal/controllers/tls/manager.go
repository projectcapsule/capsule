// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/projectcapsule/capsule/pkg/runtime/cert"
	capsuleclient "github.com/projectcapsule/capsule/pkg/runtime/client"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

const (
	certificateExpirationThreshold = 3 * 24 * time.Hour
	certificateValidity            = 6 * 30 * 24 * time.Hour

	// caPrivateKeyKey is intentionally not a Kubernetes core constant.
	// The TLS Secret remains type kubernetes.io/tls, but we persist the CA key
	// so serving cert renewal does not require CA rotation.
	caPrivateKeyKey = "ca.key"
)

type Reconciler struct {
	client.Client

	Log           logr.Logger
	Scheme        *runtime.Scheme
	Namespace     string
	Configuration configuration.Configuration
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueFn := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Namespace: r.Namespace,
					Name:      r.Configuration.TLSSecretName(),
				},
			},
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(
			&corev1.Secret{},
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{
					Names: []string{r.Configuration.TLSSecretName()},
				},
			),
		).
		Named("capsule/tls").
		Watches(
			&admissionregistrationv1.ValidatingWebhookConfiguration{},
			enqueueFn,
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{
					Names: []string{string(r.Configuration.Admission().Validating.Name)},
				},
			),
		).
		Watches(
			&admissionregistrationv1.MutatingWebhookConfiguration{},
			enqueueFn,
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{
					Names: []string{string(r.Configuration.Admission().Mutating.Name)},
				},
			),
		).
		Watches(
			&apiextensionsv1.CustomResourceDefinition{},
			enqueueFn,
			builder.WithPredicates(
				predicates.NamesMatchingPredicate{
					Names: r.managedCRDNames(),
				},
			),
		).
		Complete(r)
}

func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	r.Log = r.Log.WithValues(
		"Request.Namespace", request.Namespace,
		"Request.Name", request.Name,
	)

	if request.Namespace == "" {
		request.Namespace = r.Namespace
	}

	if request.Name == "" {
		request.Name = r.Configuration.TLSSecretName()
	}

	certSecret := &corev1.Secret{}
	if err := r.Get(ctx, request.NamespacedName, certSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}

		certSecret = &corev1.Secret{}
		certSecret.Name = request.Name
		certSecret.Namespace = request.Namespace
		certSecret.Data = map[string][]byte{}
	}

	if err := r.ReconcileCertificates(ctx, certSecret); err != nil {
		return ctrl.Result{}, err
	}

	servingCert, err := cert.GetCertificateFromBytes(certSecret.Data[corev1.TLSCertKey])
	if err != nil {
		return ctrl.Result{}, err
	}

	requeueTime := servingCert.NotAfter.Add(-(certificateExpirationThreshold - time.Second))

	requeueAfter := max(time.Until(requeueTime), 0)

	r.Log.V(4).Info("TLS reconciliation completed", "requeueAfter", requeueAfter.String())

	return ctrl.Result{
		Requeue:      true,
		RequeueAfter: requeueAfter,
	}, nil
}

func (r *Reconciler) ReconcileCertificates(ctx context.Context, certSecret *corev1.Secret) error {
	dnsName := r.webhookDNSName()

	ca, caBundle, rotateServingCert, err := r.ensureCertificateMaterial(certSecret, dnsName)
	if err != nil {
		return err
	}

	if rotateServingCert {
		if ca == nil {
			return fmt.Errorf("cannot rotate serving certificate without CA private key")
		}

		r.Log.V(3).Info("Generating new serving TLS certificate", "dnsName", dnsName)

		crt, key, err := ca.GenerateCertificate(cert.NewCertOpts(
			time.Now().Add(certificateValidity),
			dnsName,
		))
		if err != nil {
			r.Log.Error(err, "cannot generate serving TLS certificate")

			return err
		}

		certSecret.Data[corev1.TLSCertKey] = crt.Bytes()
		certSecret.Data[corev1.TLSPrivateKeyKey] = key.Bytes()

		if err := r.validateSecretCertificate(certSecret, dnsName); err != nil {
			return err
		}

		if err := r.upsertTLSSecret(ctx, certSecret); err != nil {
			return err
		}
	}

	caBundle = certSecret.Data[corev1.ServiceAccountRootCAKey]
	if len(caBundle) == 0 {
		return fmt.Errorf("missing %q field in %q secret", corev1.ServiceAccountRootCAKey, r.Configuration.TLSSecretName())
	}

	r.Log.V(4).Info("Patching caBundle in webhooks and managed CRD conversions")

	patchGroup, groupCtx := errgroup.WithContext(ctx)

	patchGroup.Go(func() error {
		return r.patchMutatingWebhookConfigurationCABundle(groupCtx, caBundle)
	})

	patchGroup.Go(func() error {
		return r.patchValidatingWebhookConfigurationCABundle(groupCtx, caBundle)
	})

	for key, managed := range r.conversionManagedCRDs() {
		patchGroup.Go(func() error {
			if err := r.updateManagedCustomResourceDefinition(groupCtx, managed, caBundle); err != nil {
				return fmt.Errorf("cannot update managed CRD %q (%s): %w", key, managed.Name, err)
			}

			return nil
		})
	}

	return patchGroup.Wait()
}

// ensureCertificateMaterial ensures that the Secret contains a stable CA
// certificate/key pair and decides whether the serving certificate must be
// regenerated.
//
// Important behavior:
//   - Missing Secret or missing ca.key creates a new CA.
//   - Existing valid CA is reused.
//   - Serving certificate renewal never rotates the CA.
//   - Legacy Secrets without ca.key rotate once into the stable format.
func (r *Reconciler) ensureCertificateMaterial(
	certSecret *corev1.Secret,
	dnsName string,
) (*cert.CapsuleCA, []byte, bool, error) {
	if certSecret.Data == nil {
		certSecret.Data = map[string][]byte{}
	}

	caBundle := certSecret.Data[corev1.ServiceAccountRootCAKey]
	caKey := certSecret.Data[caPrivateKeyKey]
	tlsCrt := certSecret.Data[corev1.TLSCertKey]
	tlsKey := certSecret.Data[corev1.TLSPrivateKeyKey]

	hasCA := len(caBundle) > 0
	hasCAKey := len(caKey) > 0
	hasServingCert := len(tlsCrt) > 0 && len(tlsKey) > 0

	// Fresh empty Secret or completely broken Secret.
	if !hasCA || !hasServingCert {
		r.Log.Info(
			"Generating new certificate authority and serving certificate",
			"reason", "missing ca.crt or serving certificate",
			"secret", certSecret.Name,
			"namespace", certSecret.Namespace,
		)

		ca, newCABundle, newCAKey, err := generateCertificateAuthorityMaterial()
		if err != nil {
			return nil, nil, false, err
		}

		certSecret.Data[corev1.ServiceAccountRootCAKey] = newCABundle
		certSecret.Data[caPrivateKeyKey] = newCAKey

		return ca, newCABundle, true, nil
	}

	// Legacy mode:
	// The Secret has a CA and serving cert, but not the CA private key.
	// If the serving cert is still valid and chains to ca.crt, do NOT rotate now.
	// Rotating here causes a temporary caBundle/server-cert mismatch during startup.
	if !hasCAKey {
		if err := r.validateSecretCertificate(certSecret, dnsName); err == nil {
			r.Log.Info(
				"TLS Secret is using legacy CA material without ca.key; keeping existing CA and serving certificate",
				"secret", certSecret.Name,
				"namespace", certSecret.Namespace,
			)

			return nil, caBundle, false, nil
		}

		r.Log.Info(
			"TLS Secret is missing ca.key and serving certificate is invalid or expiring; rotating CA",
			"secret", certSecret.Name,
			"namespace", certSecret.Namespace,
		)

		ca, newCABundle, newCAKey, err := generateCertificateAuthorityMaterial()
		if err != nil {
			return nil, nil, false, err
		}

		certSecret.Data[corev1.ServiceAccountRootCAKey] = newCABundle
		certSecret.Data[caPrivateKeyKey] = newCAKey

		return ca, newCABundle, true, nil
	}

	ca, err := cert.NewCertificateAuthorityFromBytes(caBundle, caKey)
	if err != nil {
		r.Log.Error(err, "existing CA material is invalid, regenerating CA")

		newCA, newCABundle, newCAKey, err := generateCertificateAuthorityMaterial()
		if err != nil {
			return nil, nil, false, err
		}

		certSecret.Data[corev1.ServiceAccountRootCAKey] = newCABundle
		certSecret.Data[caPrivateKeyKey] = newCAKey

		return newCA, newCABundle, true, nil
	}

	if err := validateCAKeyPair(caBundle, caKey); err != nil {
		r.Log.Error(err, "existing CA certificate/key pair is invalid, regenerating CA")

		newCA, newCABundle, newCAKey, err := generateCertificateAuthorityMaterial()
		if err != nil {
			return nil, nil, false, err
		}

		certSecret.Data[corev1.ServiceAccountRootCAKey] = newCABundle
		certSecret.Data[caPrivateKeyKey] = newCAKey

		return newCA, newCABundle, true, nil
	}

	if err := r.validateSecretCertificate(certSecret, dnsName); err != nil {
		r.Log.Info("serving certificate requires renewal", "reason", err.Error())

		return ca, caBundle, true, nil
	}

	r.Log.V(4).Info("Skipping TLS certificate generation as existing certificate is valid")

	return ca, caBundle, false, nil
}

func generateCertificateAuthorityMaterial() (*cert.CapsuleCA, []byte, []byte, error) {
	ca, err := cert.GenerateCertificateAuthority()
	if err != nil {
		return nil, nil, nil, err
	}

	caCrt, err := ca.CACertificatePem()
	if err != nil {
		return nil, nil, nil, err
	}

	caKey, err := ca.CAPrivateKeyPem()
	if err != nil {
		return nil, nil, nil, err
	}

	return ca, caCrt.Bytes(), caKey.Bytes(), nil
}

func (r *Reconciler) upsertTLSSecret(ctx context.Context, certSecret *corev1.Secret) error {
	desired := &corev1.Secret{
		ObjectMeta: certSecret.ObjectMeta,
		Type:       corev1.SecretTypeTLS,
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		if desired.Labels == nil {
			desired.Labels = map[string]string{}
		}

		if desired.Annotations == nil {
			desired.Annotations = map[string]string{}
		}

		desired.Data = copySecretData(certSecret.Data)

		return nil
	})
	if err != nil {
		r.Log.Error(err, "cannot update Capsule TLS Secret")

		return err
	}

	certSecret.ObjectMeta = desired.ObjectMeta
	certSecret.Data = copySecretData(desired.Data)

	return nil
}

func (r *Reconciler) validateSecretCertificate(secret *corev1.Secret, dnsName string) error {
	if secret == nil {
		return fmt.Errorf("secret is nil")
	}

	if secret.Data == nil {
		return fmt.Errorf("secret data is nil")
	}

	caBundle := secret.Data[corev1.ServiceAccountRootCAKey]
	if len(caBundle) == 0 {
		return fmt.Errorf("missing %q", corev1.ServiceAccountRootCAKey)
	}

	leafPEM := secret.Data[corev1.TLSCertKey]
	if len(leafPEM) == 0 {
		return fmt.Errorf("missing %q", corev1.TLSCertKey)
	}

	keyPEM := secret.Data[corev1.TLSPrivateKeyKey]
	if len(keyPEM) == 0 {
		return fmt.Errorf("missing %q", corev1.TLSPrivateKeyKey)
	}

	leaf, key, err := cert.GetCertificateWithPrivateKeyFromBytes(leafPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("cannot parse serving certificate/key pair: %w", err)
	}

	if err := cert.ValidateCertificate(leaf, key, certificateExpirationThreshold); err != nil {
		return fmt.Errorf("serving certificate is invalid or expiring: %w", err)
	}

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caBundle) {
		return fmt.Errorf("cannot parse caBundle")
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName: dnsName,
		Roots:   roots,
		KeyUsages: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
	}); err != nil {
		return fmt.Errorf("serving certificate does not verify against caBundle: %w", err)
	}

	return nil
}

func validateCAKeyPair(caCertPEM, caKeyPEM []byte) error {
	caCert, caKey, err := cert.GetCertificateWithPrivateKeyFromBytes(caCertPEM, caKeyPEM)
	if err != nil {
		return fmt.Errorf("cannot parse CA certificate/key pair: %w", err)
	}

	if !caCert.IsCA {
		return fmt.Errorf("ca.crt is not a CA certificate")
	}

	if !publicKeysEqual(caCert.PublicKey, &caKey.PublicKey) {
		return fmt.Errorf("ca.crt does not match ca.key")
	}

	now := time.Now()
	if now.Before(caCert.NotBefore) {
		return fmt.Errorf("CA certificate is not valid yet")
	}

	if now.After(caCert.NotAfter.Add(-certificateExpirationThreshold)) {
		return fmt.Errorf("CA certificate expired or expires soon")
	}

	return nil
}

func publicKeysEqual(a any, b *rsa.PublicKey) bool {
	pub, ok := a.(*rsa.PublicKey)
	if !ok {
		return false
	}

	return pub.Equal(b)
}

//nolint:dupl
func (r *Reconciler) patchValidatingWebhookConfigurationCABundle(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		vw := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		if err := r.Get(ctx, types.NamespacedName{
			Name: string(r.Configuration.Admission().Validating.Name),
		}, vw); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		patches := r.validatingWebhookCABundlePatches(vw.Webhooks, caBundle)
		if len(patches) == 0 {
			return nil
		}

		return capsuleclient.ApplyPatches(ctx, r.Client, vw, patches, "capsule-tls-controller")
	})
}

//nolint:dupl
func (r *Reconciler) patchMutatingWebhookConfigurationCABundle(ctx context.Context, caBundle []byte) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		mw := &admissionregistrationv1.MutatingWebhookConfiguration{}
		if err := r.Get(ctx, types.NamespacedName{
			Name: string(r.Configuration.Admission().Mutating.Name),
		}, mw); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		patches := r.mutatingWebhookCABundlePatches(mw.Webhooks, caBundle)
		if len(patches) == 0 {
			return nil
		}

		return capsuleclient.ApplyPatches(ctx, r.Client, mw, patches, "capsule-tls-controller")
	})
}

func (r *Reconciler) updateManagedCustomResourceDefinition(
	ctx context.Context,
	managed ManagedCRD,
	caBundle []byte,
) error {
	if !managed.ManageConversion {
		return nil
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := r.Get(ctx, types.NamespacedName{Name: managed.Name}, crd); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		before := crd.DeepCopy()

		path := managed.ConversionPath
		if path == "" {
			path = "/convert"
		}

		versions := managed.ConversionReviewVersions
		if len(versions) == 0 {
			versions = []string{"v1", "v1beta1"}
		}

		port := int32(443)

		crd.Spec.Conversion = &apiextensionsv1.CustomResourceConversion{
			Strategy: apiextensionsv1.WebhookConverter,
			Webhook: &apiextensionsv1.WebhookConversion{
				ClientConfig: &apiextensionsv1.WebhookClientConfig{
					Service: &apiextensionsv1.ServiceReference{
						Namespace: r.Namespace,
						Name:      r.Configuration.Admission().ServiceName,
						Path:      &path,
						Port:      &port,
					},
					CABundle: caBundle,
				},
				ConversionReviewVersions: versions,
			},
		}

		return r.Patch(ctx, crd, client.MergeFrom(before))
	})
}

func (r *Reconciler) webhookDNSName() string {
	return fmt.Sprintf("%s.%s.svc", r.Configuration.Admission().ServiceName, r.Namespace)
}

func copySecretData(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))

	for key, value := range in {
		out[key] = append([]byte(nil), value...)
	}

	return out
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
