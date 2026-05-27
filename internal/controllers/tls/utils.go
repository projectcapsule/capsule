// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tls

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"

	capsuleclient "github.com/projectcapsule/capsule/pkg/runtime/client"
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

//nolint:dupl
func (r *Reconciler) validatingWebhookCABundlePatches(
	webhooks []admissionregistrationv1.ValidatingWebhook,
	caBundle []byte,
) []capsuleclient.JSONPatch {
	patches := make([]capsuleclient.JSONPatch, 0, len(webhooks))

	for i := range webhooks {
		if webhooks[i].ClientConfig.Service == nil {
			continue
		}

		if equalBytes(webhooks[i].ClientConfig.CABundle, caBundle) {
			continue
		}

		r.Log.V(3).Info(
			"Patching webhook caBundle",
			"webhook", webhooks[i].Name,
			"old", certFingerprint(webhooks[i].ClientConfig.CABundle),
			"new", certFingerprint(caBundle),
		)

		patches = append(patches, capsuleclient.JSONPatch{
			Operation: capsuleclient.JSONPatchAdd,
			Path:      fmt.Sprintf("/webhooks/%d/clientConfig/caBundle", i),
			Value:     caBundle,
		})
	}

	return patches
}

//nolint:dupl
func (r *Reconciler) mutatingWebhookCABundlePatches(
	webhooks []admissionregistrationv1.MutatingWebhook,
	caBundle []byte,
) []capsuleclient.JSONPatch {
	patches := make([]capsuleclient.JSONPatch, 0, len(webhooks))

	for i := range webhooks {
		if webhooks[i].ClientConfig.Service == nil {
			continue
		}

		if equalBytes(webhooks[i].ClientConfig.CABundle, caBundle) {
			continue
		}

		r.Log.V(3).Info(
			"Patching webhook caBundle",
			"webhook", webhooks[i].Name,
			"old", certFingerprint(webhooks[i].ClientConfig.CABundle),
			"new", certFingerprint(caBundle),
		)

		patches = append(patches, capsuleclient.JSONPatch{
			Operation: capsuleclient.JSONPatchAdd,
			Path:      fmt.Sprintf("/webhooks/%d/clientConfig/caBundle", i),
			Value:     caBundle,
		})
	}

	return patches
}

func certFingerprint(pemBytes []byte) string {
	sum := sha256.Sum256(pemBytes)

	return hex.EncodeToString(sum[:8])
}
