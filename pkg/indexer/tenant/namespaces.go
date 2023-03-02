// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/pkg/api"
)

type NamespacesReference struct {
	Obj client.Object
}

func (o NamespacesReference) Object() client.Object {
	return o.Obj
}

func (o NamespacesReference) Field() string {
	return ".status.namespaces"
}

//nolint:forcetypeassert
func (o NamespacesReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		return object.(api.Tenant).GetNamespaces()
	}
}
