// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

// Package serviceaccount contains indexes shared by resources which persist
// their resolved execution identity in status.request.impersonation.
package serviceaccount

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

const ReferenceFieldName = "status.request.impersonation"

func ReferenceKey(namespace, name string) string {
	if namespace == "" || name == "" {
		return ""
	}

	return namespace + "/" + name
}

type BreakRequestReference struct{}

func (BreakRequestReference) Object() client.Object {
	return &capsulev1beta2.BreakRequest{}
}

func (BreakRequestReference) Field() string {
	return ReferenceFieldName
}

func (BreakRequestReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		request := object.(*capsulev1beta2.BreakRequest) //nolint:forcetypeassert
		if request.Status.Request == nil || request.Status.Request.Impersonation == nil {
			return nil
		}

		key := ReferenceKey(
			request.Status.Request.Impersonation.Namespace.String(),
			request.Status.Request.Impersonation.Name.String(),
		)
		if key == "" {
			return nil
		}

		return []string{key}
	}
}
