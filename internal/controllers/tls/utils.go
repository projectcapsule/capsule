// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/projectcapsule/capsule/pkg/runtime/cert"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
)

type ManagedCRD struct {
	Name                     string
	ManageConversion         bool
	ConversionPath           string
	ConversionReviewVersions []string
}

func (r Reconciler) managedCRDs() map[string]ManagedCRD {
	return map[string]ManagedCRD{
		"tenants": {
			Name:                     r.Configuration.TenantCRDName(),
			ManageConversion:         true,
			ConversionPath:           "/convert",
			ConversionReviewVersions: []string{"v1", "v1beta1"},
		},
		"capsuleconfigurations": {
			Name:                     "capsuleconfigurations.capsule.clastix.io",
			ManageConversion:         true,
			ConversionPath:           "/convert",
			ConversionReviewVersions: []string{"v1", "v1beta1"},
		},
		"customquotas": {
			Name: "customquotas.capsule.clastix.io",
		},
		"globalcustomquotas": {
			Name: "globalcustomquotas.capsule.clastix.io",
		},
		"globaltenantresources": {
			Name: "globaltenantresources.capsule.clastix.io",
		},
		"quantityledgers": {
			Name: "quantityledgers.capsule.clastix.io",
		},
		"resourcepoolclaims": {
			Name: "resourcepoolclaims.capsule.clastix.io",
		},
		"resourcepools": {
			Name: "resourcepools.capsule.clastix.io",
		},
		"rulestatuses": {
			Name: "rulestatuses.capsule.clastix.io",
		},
		"tenantowners": {
			Name: "tenantowners.capsule.clastix.io",
		},
		"tenantresources": {
			Name: "tenantresources.capsule.clastix.io",
		},
	}
}

func (r Reconciler) managedCRDNames() []string {
	crds := r.managedCRDs()

	names := make([]string, 0, len(crds))

	for _, crd := range crds {
		names = append(names, crd.Name)
	}

	return names
}

func (r Reconciler) conversionManagedCRDs() map[string]ManagedCRD {
	crds := r.managedCRDs()

	out := make(map[string]ManagedCRD, len(crds))

	for key, crd := range crds {
		if crd.ManageConversion {
			out[key] = crd
		}
	}

	return out
}

// Collects required SANs for certificate
// desiredWebhookSANs collects required SANs for the webhook serving certificate.
func (r *Reconciler) desiredWebhookSANs(ctx context.Context) (cert.CertificateSANs, error) {
	sans := cert.CertificateSANs{}

	sans.AddDNSNames(
		r.Configuration.Admission().ServiceName,
		fmt.Sprintf("%s.%s", r.Configuration.Admission().ServiceName, r.Namespace),
		fmt.Sprintf("%s.%s.svc", r.Configuration.Admission().ServiceName, r.Namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", r.Configuration.Admission().ServiceName, r.Namespace),
	)

	mutating := r.Configuration.Admission().Mutating.Client
	if mutating != nil {
		sans.AddServiceReference(mutating.Service, configuration.ControllerNamespace())

		if err := sans.AddURL(mutating.URL); err != nil {
			return cert.CertificateSANs{}, fmt.Errorf("mutating admission client URL: %w", err)
		}
	}

	validating := r.Configuration.Admission().Validating.Client
	if validating != nil {
		sans.AddServiceReference(validating.Service, configuration.ControllerNamespace())

		if err := sans.AddURL(validating.URL); err != nil {
			return cert.CertificateSANs{}, fmt.Errorf("validating admission client URL: %w", err)
		}
	}

	sans = sans.Normalize()

	if sans.Empty() {
		return cert.CertificateSANs{}, fmt.Errorf("no webhook SANs could be resolved")
	}

	r.Log.V(5).Info(
		"Evaluated required SANs for TLS controller",
		"dnsNames", sans.DNSNames,
		"ipAddresses", cert.IPsToStrings(sans.IPAddrs),
	)

	return sans, nil
}

func FetchCurrentCaBundleForAdmission(
	ctx context.Context,
	c client.Reader,
	cfg configuration.Configuration,
	configuredCABundle []byte,
) ([]byte, error) {
	// Explicit configuration wins.
	if len(configuredCABundle) > 0 {
		return append([]byte(nil), configuredCABundle...), nil
	}

	// Internal Capsule TLS enabled: source of truth is the TLS Secret.
	if cfg.EnableTLSConfiguration() {
		secret := &corev1.Secret{}

		if err := c.Get(ctx, types.NamespacedName{
			Namespace: configuration.ControllerNamespace(),
			Name:      cfg.TLSSecretName(),
		}, secret); err != nil {
			return nil, fmt.Errorf("get TLS Secret %s/%s: %w",
				configuration.ControllerNamespace(),
				cfg.TLSSecretName(),
				err,
			)
		}

		caBundle := secret.Data[corev1.ServiceAccountRootCAKey]
		if len(caBundle) == 0 {
			return nil, fmt.Errorf("TLS Secret %s/%s missing %q",
				secret.Namespace,
				secret.Name,
				corev1.ServiceAccountRootCAKey,
			)
		}

		return append([]byte(nil), caBundle...), nil
	}

	// cert-manager / external injector mode:
	// return nil and preserve current webhook caBundle.
	return nil, nil
}
