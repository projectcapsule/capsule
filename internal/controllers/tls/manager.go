// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

//nolint:nestif
package tls

import (
	"bytes"
	"context"
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
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/runtime/predicates"
)

const (
	certificateExpirationThreshold = 3 * 24 * time.Hour
	certificateValidity            = 6 * 30 * 24 * time.Hour
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
					Namespace: configuration.ControllerNamespace(),
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
	log := r.Log.WithValues(
		"Request.Namespace", request.Namespace,
		"Request.Name", request.Name,
	)

	if request.Namespace == "" {
		request.Namespace = r.Namespace
	}

	if request.Name == "" {
		request.Name = r.Configuration.TLSSecretName()
	}

	log.V(4).Info("TLS reconciliation started")

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

	if err := r.ReconcileCertificates(ctx, log, certSecret); err != nil {
		return ctrl.Result{}, err
	}

	servingCert, err := cert.GetCertificateFromBytes(certSecret.Data[corev1.TLSCertKey])
	if err != nil {
		return ctrl.Result{}, err
	}

	requeueTime := servingCert.NotAfter.Add(-(certificateExpirationThreshold - time.Second))

	requeueAfter := max(time.Until(requeueTime), 0)

	log.V(4).Info("TLS reconciliation completed", "requeueAfter", requeueAfter.String())

	return ctrl.Result{
		Requeue:      true,
		RequeueAfter: requeueAfter,
	}, nil
}

func (r *Reconciler) ReconcileCertificates(
	ctx context.Context,
	log logr.Logger,
	certSecret *corev1.Secret,
) error {
	sans, err := r.desiredWebhookSANs(ctx)
	if err != nil {
		return err
	}

	log.V(4).Info(
		"Resolved desired webhook certificate SANs",
		"dnsNames", sans.DNSNames,
		"ipAddresses", cert.IPsToStrings(sans.IPAddrs),
	)

	ca, caBundle, rotateServingCert, err := r.ensureCertificateMaterial(log, certSecret, sans)
	if err != nil {
		return err
	}

	log.V(4).Info(
		"certificate requies rotation",
		"rotation", rotateServingCert,
	)

	if rotateServingCert {
		if ca == nil {
			return fmt.Errorf("cannot rotate serving certificate without CA private key")
		}

		crt, key, err := ca.GenerateCertificate(cert.NewCertOpts(
			time.Now().Add(certificateValidity),
			sans,
		))
		if err != nil {
			return err
		}

		if err != nil {
			log.Error(err, "cannot generate serving TLS certificate")

			return err
		}

		certSecret.Data[corev1.TLSCertKey] = crt.Bytes()
		certSecret.Data[corev1.TLSPrivateKeyKey] = key.Bytes()

		if err := r.validateSecretCertificate(certSecret, sans); err != nil {
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

	log.V(5).Info("Patching caBundle in webhooks and managed CRD conversions")

	patchGroup, groupCtx := errgroup.WithContext(ctx)

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
	log logr.Logger,
	certSecret *corev1.Secret,
	sans cert.CertificateSANs,
) (*cert.CapsuleCA, []byte, bool, error) {
	sans = sans.Normalize()

	if sans.Empty() {
		return nil, nil, false, fmt.Errorf("cannot ensure TLS material without SANs")
	}

	if certSecret.Data == nil {
		certSecret.Data = map[string][]byte{}
	}

	caBundle := certSecret.Data[corev1.ServiceAccountRootCAKey]
	caKey := certSecret.Data["ca.key"]

	hasCABundle := len(caBundle) > 0
	hasCAKey := len(caKey) > 0

	var ca *cert.CapsuleCA

	rotateServingCert := false

	switch {
	case hasCABundle && hasCAKey:
		loadedCA, err := cert.NewCertificateAuthorityFromBytes(caBundle, caKey)
		if err != nil {
			log.V(3).Info(
				"Existing CA material is invalid, generating new CA",
				"error", err.Error(),
			)

			generatedCA, generatedCABundle, generatedCAKey, err := generateCertificateAuthorityMaterial()
			if err != nil {
				return nil, nil, false, err
			}

			ca = generatedCA
			caBundle = generatedCABundle

			certSecret.Data[corev1.ServiceAccountRootCAKey] = generatedCABundle
			certSecret.Data["ca.key"] = generatedCAKey

			rotateServingCert = true
		} else {
			ca = loadedCA
		}

	case hasCABundle && !hasCAKey:
		// Legacy mode: we can validate and patch caBundle, but we cannot issue
		// a new serving certificate without the CA private key.
		log.V(10).Info(
			"TLS Secret contains CA bundle but no CA private key; running in legacy CA mode",
			"secret", client.ObjectKeyFromObject(certSecret).String(),
		)

		if err := r.validateSecretCertificate(certSecret, sans); err != nil {
			return nil, nil, false, fmt.Errorf(
				"TLS Secret %s contains legacy CA material without ca.key and the serving certificate is invalid: %w",
				client.ObjectKeyFromObject(certSecret).String(),
				err,
			)
		}

		return nil, caBundle, false, nil

	default:
		log.V(10).Info(
			"TLS Secret is missing CA material, generating new CA",
			"secret", client.ObjectKeyFromObject(certSecret).String(),
		)

		generatedCA, generatedCABundle, generatedCAKey, err := generateCertificateAuthorityMaterial()
		if err != nil {
			return nil, nil, false, err
		}

		ca = generatedCA
		caBundle = generatedCABundle

		certSecret.Data[corev1.ServiceAccountRootCAKey] = generatedCABundle
		certSecret.Data["ca.key"] = generatedCAKey

		rotateServingCert = true
	}

	servingCertPEM := certSecret.Data[corev1.TLSCertKey]
	servingKeyPEM := certSecret.Data[corev1.TLSPrivateKeyKey]

	if len(servingCertPEM) == 0 || len(servingKeyPEM) == 0 {
		log.V(10).Info(
			"TLS Secret is missing serving certificate material, rotating serving certificate",
			"secret", client.ObjectKeyFromObject(certSecret).String(),
		)

		rotateServingCert = true
	} else {
		servingCert, err := cert.GetCertificateFromBytes(servingCertPEM)
		if err != nil {
			log.V(10).Info(
				"Failed to parse serving certificate, rotating serving certificate",
				"secret", client.ObjectKeyFromObject(certSecret).String(),
				"error", err.Error(),
			)

			rotateServingCert = true
		} else {
			if time.Until(servingCert.NotAfter) <= certificateExpirationThreshold {
				log.V(10).Info(
					"Serving certificate is close to expiry, rotating serving certificate",
					"secret", client.ObjectKeyFromObject(certSecret).String(),
					"notAfter", servingCert.NotAfter,
				)

				rotateServingCert = true
			}

			if !sans.MatchesCertificate(servingCert) {
				log.V(3).Info(
					"Serving certificate SANs differ from desired SANs, rotating serving certificate",
					"secret", client.ObjectKeyFromObject(certSecret).String(),
					"desiredDNSNames", sans.DNSNames,
					"desiredIPAddresses", cert.IPsToStrings(sans.IPAddrs),
					"currentDNSNames", servingCert.DNSNames,
					"currentIPAddresses", cert.IPsToStrings(servingCert.IPAddresses),
				)

				rotateServingCert = true
			}

			if err := r.validateSecretCertificate(certSecret, sans); err != nil {
				log.V(10).Info(
					"Serving certificate failed validation, rotating serving certificate",
					"secret", client.ObjectKeyFromObject(certSecret).String(),
					"error", err.Error(),
				)

				rotateServingCert = true
			}
		}
	}

	if rotateServingCert && ca == nil {
		return nil, nil, false, fmt.Errorf(
			"cannot rotate serving certificate for TLS Secret %s without CA private key",
			client.ObjectKeyFromObject(certSecret).String(),
		)
	}

	return ca, caBundle, rotateServingCert, nil
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

func (r *Reconciler) validateSecretCertificate(
	certSecret *corev1.Secret,
	sans cert.CertificateSANs,
) error {
	caPEM := certSecret.Data[corev1.ServiceAccountRootCAKey]
	if len(caPEM) == 0 {
		return fmt.Errorf("missing %q in TLS Secret %s/%s",
			corev1.ServiceAccountRootCAKey,
			certSecret.Namespace,
			certSecret.Name,
		)
	}

	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM(caPEM); !ok {
		return fmt.Errorf("failed to parse %q in TLS Secret %s/%s",
			corev1.ServiceAccountRootCAKey,
			certSecret.Namespace,
			certSecret.Name,
		)
	}

	leaf, err := cert.GetCertificateFromBytes(certSecret.Data[corev1.TLSCertKey])
	if err != nil {
		return fmt.Errorf("parse serving certificate from TLS Secret %s/%s: %w",
			certSecret.Namespace,
			certSecret.Name,
			err,
		)
	}

	normalized := sans.Normalize()
	if normalized.Empty() {
		return fmt.Errorf("cannot validate serving certificate without desired SANs")
	}

	for _, dnsName := range normalized.DNSNames {
		if _, err := leaf.Verify(x509.VerifyOptions{
			DNSName: dnsName,
			Roots:   roots,
			KeyUsages: []x509.ExtKeyUsage{
				x509.ExtKeyUsageServerAuth,
			},
		}); err != nil {
			return fmt.Errorf("serving certificate is not valid for DNS SAN %q: %w", dnsName, err)
		}
	}

	for _, ip := range normalized.IPAddrs {
		if _, err := leaf.Verify(x509.VerifyOptions{
			DNSName: ip.String(),
			Roots:   roots,
			KeyUsages: []x509.ExtKeyUsage{
				x509.ExtKeyUsageServerAuth,
			},
		}); err != nil {
			return fmt.Errorf("serving certificate is not valid for IP SAN %q: %w", ip.String(), err)
		}
	}

	return nil
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

		// Only patch CRDs that already use webhook conversion.
		if crd.Spec.Conversion == nil ||
			crd.Spec.Conversion.Webhook == nil ||
			crd.Spec.Conversion.Webhook.ClientConfig == nil {
			return nil
		}

		current := crd.Spec.Conversion.Webhook.ClientConfig.CABundle
		if bytes.Equal(current, caBundle) {
			return nil
		}

		before := crd.DeepCopy()

		crd.Spec.Conversion.Webhook.ClientConfig.CABundle = append([]byte(nil), caBundle...)

		return r.Patch(ctx, crd, client.MergeFrom(before))
	})
}

func copySecretData(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))

	for key, value := range in {
		out[key] = append([]byte(nil), value...)
	}

	return out
}
