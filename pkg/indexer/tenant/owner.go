// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/utils"
)

type OwnerReference struct{}

func (o OwnerReference) Object() client.Object {
	return &capsulev1beta2.Tenant{}
}

func (o OwnerReference) Field() string {
	return ".spec.owner.ownerkind"
}

func (o OwnerReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tenant, ok := object.(*capsulev1beta2.Tenant)
		if !ok {
			panic(fmt.Errorf("expected type *capsulev1beta2.Tenant, got %T", tenant))
		}

		return utils.GetOwnersWithKinds(tenant)
	}
}
