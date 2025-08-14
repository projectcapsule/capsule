// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resourcepool

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type PoolUIDReference struct {
	Obj client.Object
}

func (o PoolUIDReference) Object() client.Object {
	return o.Obj
}

func (o PoolUIDReference) Field() string {
	return ".status.pool.uid"
}

func (o PoolUIDReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		grq, ok := object.(*capsulev1beta2.ResourcePoolClaim)
		if !ok {
			return nil
		}

		return []string{string(grq.Status.Pool.UID)}
	}
}
