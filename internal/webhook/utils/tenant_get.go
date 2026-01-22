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
	"github.com/projectcapsule/capsule/pkg/configuration"
	"github.com/projectcapsule/capsule/pkg/utils/tenant"
)

// getNamespaceTenant returns namespace owner tenant.
func GetNamespaceTenant(
	ctx context.Context,
	client client.Client,
	ns *corev1.Namespace,
	req admission.Request,
	cfg configuration.Configuration,
	recorder events.EventRecorder,
) (*capsulev1beta2.Tenant, *admission.Response) {
	tnt, err := tenant.GetTenantByLabelsAndUser(ctx, client, cfg, ns, req.UserInfo)
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return nil, &response
	}

	if tnt != nil {
		return tnt, nil
	}

	tnts, err := tenant.GetTenantByUserInfo(ctx, client, cfg, ns, req.UserInfo.Username, req.UserInfo.Groups)
	if err != nil {
		response := admission.Errored(http.StatusBadRequest, err)

		return nil, &response
	}

	if len(tnts) == 0 {
		response := admission.Denied("You do not have any Tenant assigned: please, reach out to the system administrators")

		return nil, &response
	}

	if len(tnts) == 1 {
		// Check if namespace needs Tenant name prefix
		if !validateNamespacePrefix(ns, &tnts[0]) {
			response := admission.Denied(fmt.Sprintf("The Namespace name must start with '%s-' when ForceTenantPrefix is enabled in the Tenant.", tnts[0].GetName()))

			return nil, &response
		}

		return &tnts[0], nil
	}

	if cfg.ForceTenantPrefix() {
		for _, t := range tnts {
			if strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", t.GetName())) {
				return &t, nil
			}
		}

		response := admission.Denied("The Namespace prefix used doesn't match any available Tenant")

		return nil, &response
	}

	return nil, nil
}

func validateNamespacePrefix(ns *corev1.Namespace, tenant *capsulev1beta2.Tenant) bool {
	// Check if ForceTenantPrefix is true
	if tenant.Spec.ForceTenantPrefix != nil && *tenant.Spec.ForceTenantPrefix {
		if !strings.HasPrefix(ns.GetName(), fmt.Sprintf("%s-", tenant.GetName())) {
			return false
		}
	}

	return true
}
