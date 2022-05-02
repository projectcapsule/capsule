// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

type NamespacesReference struct{}

func (o NamespacesReference) Object() client.Object {
	return &capsulev1beta1.Tenant{}
}

func (o NamespacesReference) Field() string {
	return ".status.namespaces"
}

// nolint:forcetypeassert
func (o NamespacesReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		namespaces := object.(*capsulev1beta1.Tenant).DeepCopy().Status.Namespaces

		if namespaces == nil {
			return []string{}
		}

		return namespaces
	}
}
