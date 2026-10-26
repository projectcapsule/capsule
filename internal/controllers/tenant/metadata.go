// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// Sets a label on the Tenant object with it's name.
func (r *Manager) ensureMetadata(ctx context.Context, tnt *capsulev1beta2.Tenant) (err error) {
	if tnt.Labels == nil {
		tnt.Labels = map[string]string{}
	}

	if v, ok := tnt.Labels[meta.TenantNameLabel]; !ok || v != tnt.Name {
		tnt.Labels[meta.TenantNameLabel] = tnt.Name
	}

	// Admission installs this before the Tenant can acquire namespaces. Repair
	// older Tenants too, but never add a finalizer after deletion has started.
	if tnt.DeletionTimestamp == nil {
		controllerutil.AddFinalizer(tnt, meta.ControllerFinalizer)
	}

	return nil
}

// finalizeTenant runs only after child reconciliation succeeds. Status is a
// work list, not proof of absence: admission may have assigned a namespace before
// its first reconciliation. A cached index cannot prove absence either.
func (r *Manager) finalizeTenant(ctx context.Context, tnt *capsulev1beta2.Tenant) error {
	if tnt.DeletionTimestamp == nil || !controllerutil.ContainsFinalizer(tnt, meta.ControllerFinalizer) || len(tnt.Status.Spaces) != 0 {
		return nil
	}

	// Only the final deletion transition pays for this scan. Metadata-only pages
	// bound response size; no scan occurs on admission, active reconciliation, or
	// repeated deletion reconciles while known namespaces still exist. Labels
	// cannot select legacy/unlabelled namespaces, so check owner UIDs directly.
	options := &client.ListOptions{Limit: 500}

	for {
		namespaces := &metav1.PartialObjectMetadataList{}
		namespaces.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("NamespaceList"))

		if err := r.reader.List(ctx, namespaces, options); err != nil {
			return fmt.Errorf("verify remaining Tenant namespaces: %w", err)
		}

		for _, ns := range namespaces.Items {
			for _, ref := range ns.OwnerReferences {
				if !tenant.IsTenantOwnerReferenceForTenant(ref, tnt) {
					continue
				}

				// Spaces started empty and names in a LIST page are unique.
				tnt.Status.Spaces = append(tnt.Status.Spaces, &capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns.Name, UID: ns.UID, Conditions: meta.ConditionList{},
				})

				break
			}
		}

		// Hand a bounded batch to the existing deletion workers. Restart the
		// authoritative scan once it has drained, including after a restart.
		if len(tnt.Status.Spaces) > 0 {
			tnt.Status.Size = uint(len(tnt.Status.Spaces))
			tnt.Status.Namespaces = make([]string, 0, len(tnt.Status.Spaces))

			for _, ns := range tnt.Status.Spaces {
				tnt.Status.Namespaces = append(tnt.Status.Namespaces, ns.Name)
			}

			return nil
		}

		options.Continue = namespaces.Continue
		if options.Continue == "" {
			break
		}
	}

	controllerutil.RemoveFinalizer(tnt, meta.ControllerFinalizer)

	return nil
}
