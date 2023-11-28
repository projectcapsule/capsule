// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) ensureMetadata(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	// Assign Labels
	if tnt.Labels == nil {
		tnt.Labels = make(map[string]string)
	}

	tnt.Labels[capsuleapi.TenantNameLabel] = tnt.Name

	return r.Client.Update(ctx, tnt)
}
