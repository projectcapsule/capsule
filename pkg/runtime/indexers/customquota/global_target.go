// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package customquota

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

type GlobalTargetReference struct{}

func (o GlobalTargetReference) Object() client.Object {
	return &capsulev1beta2.GlobalCustomQuota{}
}

func (o GlobalTargetReference) Field() string {
	return TargetIndexerFieldName
}

//nolint:forcetypeassert
func (o GlobalTargetReference) Func() client.IndexerFunc {
	return func(object client.Object) []string {
		tr := object.(*capsulev1beta2.GlobalCustomQuota) //nolint:forcetypeassert

		targets := []string{}
		for _, t := range tr.Status.Targets {
			targets = append(targets, t.String())

		}

		return targets
	}
}
