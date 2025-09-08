// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	capsuleapi "github.com/projectcapsule/capsule/pkg/api"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) ensureMetadata(ctx context.Context, tenant *capsulev1beta2.Tenant) (err error) {
	// Assign Labels
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() (retryErr error) {
		tnt := &capsulev1beta2.Tenant{}
		if retryErr = r.Get(ctx, types.NamespacedName{Name: tenant.GetName()}, tnt); retryErr != nil {
			return
		}
		if tnt.Labels == nil {
			tnt.Labels = make(map[string]string)
		}

		tnt.Labels[capsuleapi.TenantNameLabel] = tnt.Name

		return r.Update(ctx, tnt)
	})
	return err
}
