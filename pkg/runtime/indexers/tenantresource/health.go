// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

const GlobalHealthNamespacesFieldName = ".spec.healthNamespaceSelection"

const GlobalNamespaceProfileIndexValue = "profile"

// GlobalHealthNamespaces narrows namespace events to health-enabled sources and
// sources with namespace profiles. The index never authorizes target access.
type GlobalHealthNamespaces struct{}

func (GlobalHealthNamespaces) Object() client.Object { return &capsulev1beta2.GlobalTenantResource{} }
func (GlobalHealthNamespaces) Field() string         { return GlobalHealthNamespacesFieldName }
func (GlobalHealthNamespaces) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		resource, ok := obj.(*capsulev1beta2.GlobalTenantResource)
		if !ok || resource.Spec.Scope != api.ResourceScopeNamespace {
			return nil
		}

		var keys []string
		if resource.Spec.HealthChecks.Enabled() {
			keys = append(keys, "true")
		}

		for _, spec := range resource.Spec.Resources {
			if spec.NamespaceSelector != nil {
				keys = append(keys, GlobalNamespaceProfileIndexValue)

				break
			}
		}

		return keys
	}
}
