// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/configuration"
	caputils "github.com/projectcapsule/capsule/pkg/utils"
)

func SetGlobalTenantResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.GlobalTenantResource,
) (changed bool) {

	// If name is empty, remove the whole reference
	if resource.Spec.ServiceAccount == nil || resource.Spec.ServiceAccount.Name == "" {
		// If a default is configured, apply it
		if setGlobalTenantDefaultResourceServiceAccount(config, resource) {
			changed = true
		} else {
			if resource.Spec.ServiceAccount != nil {
				resource.Spec.ServiceAccount = nil
				changed = true
			}

			return
		}
	}

	// Sanitize the Name
	sanitizedName := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Name.String())
	if resource.Spec.ServiceAccount.Name.String() != sanitizedName {
		resource.Spec.ServiceAccount.Name = api.Name(sanitizedName)
		changed = true
	}

	// Always set the namespace to match the resource
	sanitizedNS := caputils.SanitizeServiceAccountProp(resource.Namespace)
	if resource.Spec.ServiceAccount.Namespace.String() != sanitizedNS {
		resource.Spec.ServiceAccount.Namespace = api.Name(sanitizedNS)
		changed = true
	}

	return
}

func SetTenantResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.TenantResource,
) (changed bool) {
	changed = false

	// If name is empty, remove the whole reference
	if resource.Spec.ServiceAccount == nil || resource.Spec.ServiceAccount.Name == "" {
		// If a default is configured, apply it
		if setTenantDefaultResourceServiceAccount(config, resource) {
			changed = true
		} else {
			// Remove invalid ServiceAccount reference
			if resource.Spec.ServiceAccount != nil {
				resource.Spec.ServiceAccount = nil
				changed = true
			}

			return
		}
	}

	// Sanitize the Name
	sanitizedName := caputils.SanitizeServiceAccountProp(resource.Spec.ServiceAccount.Name.String())
	if resource.Spec.ServiceAccount.Name.String() != sanitizedName {
		resource.Spec.ServiceAccount.Name = api.Name(sanitizedName)
		changed = true
	}

	// Always set the namespace to match the resource
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
		caputils.SanitizeServiceAccountProp(cfg.TenantDefaultServiceAccount.String()),
	)

	return true
}

func setGlobalTenantDefaultResourceServiceAccount(
	config configuration.Configuration,
	resource *capsulev1beta2.GlobalTenantResource,
) (changed bool) {
	cfg := config.ServiceAccountClientProperties()
	if cfg == nil {
		return false
	}

	if cfg.GlobalDefaultServiceAccount == "" && cfg.GlobalDefaultServiceAccountNamespace == "" {
		return false
	}

	if resource.Spec.ServiceAccount == nil {
		resource.Spec.ServiceAccount = &api.ServiceAccountReference{}
	}

	if cfg.GlobalDefaultServiceAccount == "" {
		resource.Spec.ServiceAccount.Name = api.Name(
			caputils.SanitizeServiceAccountProp(cfg.GlobalDefaultServiceAccount.String()),
		)
	}

	if cfg.GlobalDefaultServiceAccountNamespace == "" {
		resource.Spec.ServiceAccount.Namespace = api.Name(
			caputils.SanitizeServiceAccountProp(cfg.GlobalDefaultServiceAccountNamespace.String()),
		)
	}

	return true
}
