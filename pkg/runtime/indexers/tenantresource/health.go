// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

const GlobalHealthNamespacesFieldName = ".spec.healthNamespaceSelection"

const NamespacedHealthNamespacesFieldName = ".spec.healthSourceNamespace"

type NamespacedHealthNamespaces struct{}

func (NamespacedHealthNamespaces) Object() client.Object { return &capsulev1beta2.TenantResource{} }
func (NamespacedHealthNamespaces) Field() string         { return NamespacedHealthNamespacesFieldName }

func (NamespacedHealthNamespaces) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		resource, ok := obj.(*capsulev1beta2.TenantResource)
		if !ok || !resource.Spec.HealthChecks.Enabled() {
			return nil
		}

		return []string{resource.Namespace}
	}
}

// GlobalHealthNamespaces narrows namespace profile events to opt-in global
// replications. The index selects candidates, never authorizes target access.
type GlobalHealthNamespaces struct{}

func (GlobalHealthNamespaces) Object() client.Object { return &capsulev1beta2.GlobalTenantResource{} }
func (GlobalHealthNamespaces) Field() string         { return GlobalHealthNamespacesFieldName }
func (GlobalHealthNamespaces) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		resource, ok := obj.(*capsulev1beta2.GlobalTenantResource)
		if !ok || !resource.Spec.HealthChecks.Enabled() || resource.Spec.Scope != api.ResourceScopeNamespace {
			return nil
		}

		return []string{"true"}
	}
}
