// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type NamespacedServiceAccount struct{}

func (g NamespacedServiceAccount) Object() client.Object {
	return &capsulev1beta2.TenantResource{}
}

func (g NamespacedServiceAccount) Field() string {
	return ServiceAccountIndexerFieldName
}

func (g NamespacedServiceAccount) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tgr := object.(*capsulev1beta2.TenantResource) //nolint:forcetypeassert

		imp := tgr.Status.ServiceAccount
		if imp == nil {
			return nil
		}

		ns := tgr.GetNamespace()
		name := imp.Name.String()
		if ns == "" || name == "" {
			return nil
		}

		return []string{ns + "/" + name}
	}
}
