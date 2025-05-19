// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package resourcepool

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

// NamespacesReference defines the indexer logic for GlobalResourceQuota namespaces.
type NamespacesReference struct {
	Obj client.Object
}

func (o NamespacesReference) Object() client.Object {
	return o.Obj
}

func (o NamespacesReference) Field() string {
	return ".status.namespaces"
}

func (o NamespacesReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		rp, ok := object.(*capsulev1beta2.ResourcePool)
		if !ok {
			return nil
		}

		return rp.Status.Namespaces
	}
}
