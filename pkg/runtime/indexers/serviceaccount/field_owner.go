// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package serviceaccount

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

const ResourcePermitFieldOwnerIndex = "metadata.resourcePermitFieldOwner"

// ResourcePermitFieldOwner selects immutable permit identities before any
// targets are applied. Admission verifies candidates through the API reader.
type ResourcePermitFieldOwner struct{}

func (ResourcePermitFieldOwner) Object() client.Object { return &capsulev1beta2.ResourcePermit{} }
func (ResourcePermitFieldOwner) Field() string         { return ResourcePermitFieldOwnerIndex }
func (ResourcePermitFieldOwner) Func() client.IndexerFunc {
	return func(obj client.Object) []string {
		if obj.GetUID() == "" {
			return nil
		}

		return []string{meta.ResourcePermitFieldOwner(obj)}
	}
}
