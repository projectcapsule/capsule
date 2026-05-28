// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	ad "github.com/projectcapsule/capsule/pkg/runtime/admission"
	"github.com/projectcapsule/capsule/pkg/runtime/configuration"
	"github.com/projectcapsule/capsule/pkg/tenant"
	"github.com/projectcapsule/capsule/pkg/users"
)

func GetNamespaceTenant(
	ctx context.Context,
	reader client.Reader,
	cache client.Client,
	ns *corev1.Namespace,
	user users.AdmissionUser,
	cfg configuration.Configuration,
	recorder events.EventRecorder,
) (*capsulev1beta2.Tenant, *admission.Response) {
	tnt, err := tenant.GetTenantByLabelsAndUser(ctx, reader, cfg, ns, user)
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return nil, &response
	}

	if tnt != nil {
		if !validateNamespacePrefix(cfg, ns, tnt) {
			return nil, ad.Deny(fmt.Sprintf(
				"The Namespace name must start with '%s-' when ForceTenantPrefix is enabled in the Tenant.",
				tnt.GetName(),
			))
		}

		return tnt, nil
	}

	tnts, err := tenant.GetTenantByUserInfo(ctx, cache, cfg, ns, user)
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return nil, &response
	}

	if len(tnts) == 0 {
		return nil, ad.Deny("You do not have any Tenant assigned: please, reach out to the system administrators")
	}

	if len(tnts) == 1 {
		if !validateNamespacePrefix(cfg, ns, &tnts[0]) {
			return nil, ad.Deny(fmt.Sprintf(
				"The Namespace name must start with '%s-' when ForceTenantPrefix is enabled in the Tenant.",
				tnts[0].GetName(),
			))
		}

		return &tnts[0], nil
	}

	tnt, ambiguous := resolveTenantByClosestNamespacePrefix(ns.GetName(), tnts)
	if ambiguous {
		return nil, ad.Deny("The Namespace prefix matches more than one available Tenant")
	}

	if tnt != nil {
		if !validateNamespacePrefix(cfg, ns, tnt) {
			return nil, ad.Deny(fmt.Sprintf(
				"The Namespace name must start with '%s-' when ForceTenantPrefix is enabled in the Tenant.",
				tnt.GetName(),
			))
		}

		return tnt, nil
	}

	if cfg.ForceTenantPrefix() {
		return nil, ad.Deny("The Namespace prefix used doesn't match any available Tenant")
	}

	return nil, nil
}

func resolveTenantByClosestNamespacePrefix(
	namespaceName string,
	tnts []capsulev1beta2.Tenant,
) (*capsulev1beta2.Tenant, bool) {
	var matched *capsulev1beta2.Tenant

	matchedPrefixLen := -1

	ambiguous := false

	for i := range tnts {
		prefix := fmt.Sprintf("%s-", tnts[i].GetName())
		if !strings.HasPrefix(namespaceName, prefix) {
			continue
		}

		switch {
		case len(prefix) > matchedPrefixLen:
			matched = &tnts[i]
			matchedPrefixLen = len(prefix)
			ambiguous = false

		case len(prefix) == matchedPrefixLen:
			ambiguous = true
		}
	}

	return matched, ambiguous
}

func validateNamespacePrefix(cfg configuration.Configuration, ns *corev1.Namespace, tenant *capsulev1beta2.Tenant) bool {
	enforce := cfg.ForceTenantPrefix()

	if tenant.Spec.ForceTenantPrefix != nil {
		enforce = *tenant.Spec.ForceTenantPrefix
	}

	if !enforce {
		return true
	}

	return strings.HasPrefix(ns.GetName(), tenant.GetName()+"-")
}
