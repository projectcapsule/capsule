// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) ensureMetadata(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Labels == nil {
		tnt.Labels = map[string]string{}
	}

	if v, ok := tnt.Labels[meta.TenantNameLabel]; !ok || v != tnt.Name {
		tnt.Labels[meta.TenantNameLabel] = tnt.Name
	}

	if len(tnt.Status.Spaces) == 0 {
		controllerutil.RemoveFinalizer(tnt, meta.ControllerFinalizer)
	} else {
		controllerutil.AddFinalizer(tnt, meta.ControllerFinalizer)
	}

	return nil
}
