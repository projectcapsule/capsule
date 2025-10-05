// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) ensureMetadata(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	// Assign Labels
	if tnt.Labels == nil {
		tnt.Labels = make(map[string]string)
	}

	if v, ok := tnt.Labels[capsuleapi.TenantNameLabel]; ok && v == tnt.Name {
		return
	}

	if err := r.Update(ctx, tnt); err != nil {
		return err
	}

	return r.Get(ctx, types.NamespacedName{Name: tnt.GetName()}, tnt)
}
