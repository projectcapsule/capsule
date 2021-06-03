// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/clastix/capsule/api/v1alpha1"
)

type NamespacesReference struct {
}

func (o NamespacesReference) Object() client.Object {
	return &v1alpha1.Tenant{}
}

func (o NamespacesReference) Field() string {
	return ".status.namespaces"
}

func (o NamespacesReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		return object.(*v1alpha1.Tenant).DeepCopy().Status.Namespaces
	}
}
