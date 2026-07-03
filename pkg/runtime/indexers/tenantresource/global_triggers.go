// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantresource

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// GlobalTriggers indexes GlobalTenantResources by the GroupVersionKind of each
// of their triggers, so the watch manager can resolve "which GlobalTenantResource
// cares about this kind" without listing every object.
type GlobalTriggers struct{}

func (g GlobalTriggers) Object() client.Object {
	return &capsulev1beta2.GlobalTenantResource{}
}

func (g GlobalTriggers) Field() string {
	return TriggersIndexerFieldName
}

func (g GlobalTriggers) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tgr := object.(*capsulev1beta2.GlobalTenantResource) //nolint:forcetypeassert

		return triggerIndexKeys(tgr.Spec.TenantResourceCommonSpec)
	}
}
