// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type NamespacedTargetReference struct{}

func (o NamespacedTargetReference) Object() client.Object {
	return &capsulev1beta2.CustomQuota{}
}

func (o NamespacedTargetReference) Field() string {
	return TargetIndexerFieldName
}

func (o NamespacedTargetReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tr := object.(*capsulev1beta2.CustomQuota) //nolint:forcetypeassert

		targets := []string{}
		for _, t := range tr.Status.Targets {
			targets = append(targets, t.String())
		}

		return targets
	}
}
