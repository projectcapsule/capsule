// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
	"github.com/clastix/capsule/pkg/utils"
)

type OwnerReference struct {
}

func (o OwnerReference) Object() client.Object {
	return &v1alpha1.Tenant{}
}

func (o OwnerReference) Field() string {
	return ".spec.owner.ownerkind"
}

func (o OwnerReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tenant := object.(*v1alpha1.Tenant)
		return []string{utils.GetOwnerWithKind(tenant)}
	}
}
