// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"reflect"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

// Namespace lifecycle events already enqueue all replications in the tenant.
// Profile changes select sources in the owning Tenant, including apply-only sources.
// Dispatch each namespace once
// rather than repeating both mappings (and mapping unchanged ownership twice).
func (r *namespacedResourceController) tenantNamespaceHandler() handler.EventHandler {
	enqueue := func(ctx context.Context, queue workqueue.TypedRateLimitingInterface[reconcile.Request], profileOnly bool, objects ...client.Object) {
		seen := make(map[reconcile.Request]struct{})

		for _, obj := range objects {
			if obj == nil {
				continue
			}

			var requests []reconcile.Request

			label := obj.GetLabels()[meta.TenantLabel]

			if !profileOnly && label != "" {
				requests = r.enqueueTenantResourcesForNamespace(ctx, obj)
			}

			// Owner-reference-only namespaces can also change the effective profile.
			// The profile mapper validates the live Tenant UID before selecting sources.
			if profileOnly || label == "" || !slices.ContainsFunc(obj.GetOwnerReferences(), func(ref metav1.OwnerReference) bool {
				return tenant.IsTenantOwnerReference(ref) && ref.Name == label
			}) {
				requests = append(requests, r.enqueueTenantResourcesForNamespaceProfile(ctx, obj)...)
			}

			for _, request := range requests {
				if _, exists := seen[request]; !exists {
					seen[request] = struct{}{}

					queue.Add(request)
				}
			}
		}
	}

	return handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, queue, false, e.Object)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, queue, false, e.Object)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return
			}

			membershipChanged := e.ObjectOld.GetLabels()[meta.TenantLabel] != e.ObjectNew.GetLabels()[meta.TenantLabel] || !reflect.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences())
			if membershipChanged {
				enqueue(ctx, queue, false, e.ObjectOld, e.ObjectNew)

				return
			}

			lifecycle := e.ObjectOld.GetDeletionTimestamp().IsZero() != e.ObjectNew.GetDeletionTimestamp().IsZero()
			enqueue(ctx, queue, !lifecycle, e.ObjectNew)
		},
	}
}
