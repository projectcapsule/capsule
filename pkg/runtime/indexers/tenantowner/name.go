// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenantowner

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type OwnerNameReference struct{}

func (o OwnerNameReference) Object() client.Object {
	return &capsulev1beta2.TenantOwner{}
}

func (o OwnerNameReference) Field() string {
	return NameIndexerFieldName
}

func (o OwnerNameReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		instance, ok := object.(*capsulev1beta2.TenantOwner)
		if !ok {
			return nil
		}

		if instance.Spec.Name == "" {
			return nil
		}

		return []string{instance.Spec.Name}
	}
}
