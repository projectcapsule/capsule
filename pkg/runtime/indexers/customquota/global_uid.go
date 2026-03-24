// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type GlobalObjectUIDReference struct{}

func (o GlobalObjectUIDReference) Object() client.Object {
	return &capsulev1beta2.GlobalCustomQuota{}
}

func (o GlobalObjectUIDReference) Field() string {
	return ObjectUIDIndexerFieldName
}

//nolint:forcetypeassert
func (o GlobalObjectUIDReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tr := object.(*capsulev1beta2.GlobalCustomQuota) //nolint:forcetypeassert

		objs := []string{}

		for _, obj := range tr.Status.Claims {
			objs = append(objs, string(obj.UID))
		}

		return objs
	}
}
