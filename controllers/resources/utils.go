// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"os"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
	caputils "github.com/projectcapsule/capsule/pkg/utils"
)

func SetGlobalTenantResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.GlobalTenantResource,
) (changed bool) {
	changed = false

	name := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Name.String())
	if resource.Spec.ServiceAccount.Name != "" && resource.Spec.ServiceAccount.Name.String() != name {
		resource.Spec.ServiceAccount.Name = api.Name(name)
		changed = true
	}

	if resource.Spec.ServiceAccount.Name.String() == "" {
		cfg := config.ServiceAccountClientProperties()
		if cfg == nil || cfg.TenantDefaultServiceAccount != "" {
			return
		}

		resource.Spec.ServiceAccount.Name = api.Name(caputils.SanitizeServiceAccountProp(cfg.TenantDefaultServiceAccount))
		changed = true
	}

	if resource.Spec.ServiceAccount.Namespace == "" {
		dflt := caputils.SanitizeServiceAccountProp(os.Getenv("NAMESPACE"))
		if resource.Spec.ServiceAccount.Namespace.String() != dflt {
			resource.Spec.ServiceAccount.Namespace = api.Name(dflt)
			changed = true
		}
	} else {
		ns := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Namespace.String())
		if resource.Spec.ServiceAccount.Namespace.String() != ns {
			resource.Spec.ServiceAccount.Namespace = api.Name(ns)
			changed = true
		}
	}

	return
}

func SetTenantResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.TenantResource,
) (changed bool) {
	changed = false

	if resource.Spec.ServiceAccount == nil || resource.Spec.ServiceAccount.Name == "" {
		if !setTenantDefaultResourceServiceAccount(config, resource) {
			return
		}

		changed = true
	}

	// Always sanitize the Name field (strip any colons, etc.)
	sanitizedName := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Name.String())
	if resource.Spec.ServiceAccount.Name.String() != sanitizedName {
		resource.Spec.ServiceAccount.Name = api.Name(sanitizedName)
		changed = true
	}

	if resource.Spec.ServiceAccount.Name == "" && resource.Spec.ServiceAccount.Namespace != "" {
		resource.Spec.ServiceAccount = nil
		changed = true

		return
	}

	sanitizedNS := caputils.SanitizeServiceAccountProp(resource.Namespace)
	if resource.Spec.ServiceAccount.Namespace.String() != sanitizedNS {
		resource.Spec.ServiceAccount.Namespace = api.Name(sanitizedNS)
		changed = true
	}

	return
}

func setTenantDefaultResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.TenantResource,
) (changed bool) {
	cfg := config.ServiceAccountClientProperties()
	if cfg == nil {
		return false
	}

	if cfg.TenantDefaultServiceAccount == "" {
		return false
	}

	if resource.Spec.ServiceAccount == nil {
		resource.Spec.ServiceAccount = &api.ServiceAccountReference{}
	}

	resource.Spec.ServiceAccount.Name = api.Name(
		caputils.SanitizeServiceAccountProp(cfg.TenantDefaultServiceAccount),
	)

	return true
}
