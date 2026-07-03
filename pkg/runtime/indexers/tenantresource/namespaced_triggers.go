// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// NamespacedTriggers indexes TenantResources by the GroupVersionKind of each of
// their triggers, so the watch manager can resolve "which TenantResource cares
// about this kind" without listing every object.
type NamespacedTriggers struct{}

func (g NamespacedTriggers) Object() client.Object {
	return &capsulev1beta2.TenantResource{}
}

func (g NamespacedTriggers) Field() string {
	return TriggersIndexerFieldName
}

func (g NamespacedTriggers) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tgr := object.(*capsulev1beta2.TenantResource) //nolint:forcetypeassert

		return triggerIndexKeys(tgr.Spec.TenantResourceCommonSpec)
	}
}
