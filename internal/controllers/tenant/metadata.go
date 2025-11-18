// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) ensureMetadata(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error, changed bool) {
	// Assign Labels
	if tnt.Labels == nil {
		tnt.Labels = make(map[string]string)
	}

	if v, ok := tnt.Labels[meta.TenantNameLabel]; !ok || v != tnt.Name {
		if err := r.Update(ctx, tnt); err != nil {
			return err, false
		}

		return nil, true
	}

	return nil, false
}
