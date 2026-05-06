// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type NamespacedObjectUIDReference struct{}

func (o NamespacedObjectUIDReference) Object() client.Object {
	return &capsulev1beta2.CustomQuota{}
}

func (o NamespacedObjectUIDReference) Field() string {
	return ObjectUIDIndexerFieldName
}

func (o NamespacedObjectUIDReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tr := object.(*capsulev1beta2.CustomQuota) //nolint:forcetypeassert

		objs := make([]string, 0, len(tr.Status.Claims))

		for _, obj := range tr.Status.Claims {
			objs = append(objs, string(obj.UID))
		}

		return objs
	}
}
