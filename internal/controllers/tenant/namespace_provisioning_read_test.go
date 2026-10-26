// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

func TestNamespaceMetadataCleanupAfterTransientRead(t *testing.T) {
	for _, transient := range []bool{false, true} {
		t.Run(fmt.Sprintf("transient=%v", transient), func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{AdditionalMetadataList: []api.AdditionalMetadataSelectorSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "selected"}}, Labels: map[string]string{"managed": "old"}, Annotations: map[string]string{"managed": "old"}}}}
			selected, plain := cleanupOwnedNamespace(tnt, "selected", false), cleanupOwnedNamespace(tnt, "plain", false)
			selected.Labels["profile"], selected.Labels["user"] = "selected", "keep"
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			r, _ := namespaceCleanupFixture(t, tnt, selected, plain, other, foreign)
			require.NoError(t, r.reconcileActiveTenantNamespaces(ctx, logr.Discard(), tnt))
			require.NoError(t, r.Status().Update(ctx, tnt))
			latest := &capsulev1beta2.Tenant{}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), latest))
			inventory := latest.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: selected.Name})
			require.Equal(t, "old", inventory.Metadata.Labels["managed"])
			latest.Spec.NamespaceOptions.AdditionalMetadataList = nil
			latest.Generation++
			require.NoError(t, r.Update(ctx, latest))
			if transient {
				before := latest.DeepCopy()
				failure := errors.New("transient namespace read failure")
				r.provisioningReader = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.Namespace); ok && key.Name == selected.Name {
						return failure
					}
					return c.Get(ctx, key, obj, opts...)
				}})
				err := r.reconcileActiveTenantNamespaces(ctx, logr.Discard(), latest)
				require.ErrorIs(t, err, failure)
				require.NoError(t, r.updateTenantStatus(ctx, latest, err))
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), latest))
				require.Len(t, latest.Status.Spaces, 2, "transient reads must retain the namespace inventory")
				failed := latest.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: selected.Name})
				require.Equal(t, inventory.Metadata, failed.Metadata)
				ready := failed.Conditions.GetConditionByType(meta.ReadyCondition)
				require.NotNil(t, ready)
				require.Equal(t, metav1.ConditionFalse, ready.Status)
				require.Equal(t, meta.FailedReason, ready.Reason)
				require.Contains(t, ready.Message, failure.Error())
				require.Equal(t, []string{plain.Name}, readyTenantNamespaces(latest), "independent healthy namespaces can continue")
				require.Equal(t, inventory, before.Status.GetInstance(inventory), "worker must not mutate its shared snapshot")
				r.provisioningReader = r.Client
			}
			require.NoError(t, r.reconcileActiveTenantNamespaces(ctx, logr.Discard(), latest))
			require.NoError(t, r.updateTenantStatus(ctx, latest, nil))
			actual := &corev1.Namespace{}
			for _, ns := range []*corev1.Namespace{selected, plain, foreign} {
				require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(ns), actual))
				require.NotContains(t, actual.Labels, "managed")
				require.NotContains(t, actual.Annotations, "managed")
				if ns.Name == selected.Name {
					require.Equal(t, "keep", actual.Labels["user"])
				}
			}
			require.NoError(t, r.Get(ctx, client.ObjectKeyFromObject(tnt), latest))
			require.Equal(t, latest.Generation, latest.Status.ObservedGeneration)
			require.Equal(t, metav1.ConditionTrue, latest.Status.GetInstance(inventory).Conditions.GetConditionByType(meta.ReadyCondition).Status)
		})
	}
}

func TestNamespaceProvisioningReadFailureScope(t *testing.T) {
	for _, mode := range []string{"transient", "cancelled", "new-namespace", "old-inventory-uid", "not-found", "recreated-namespace", "reassigned-namespace"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			tnt := provisioningTenant()
			ns := cleanupOwnedNamespace(tnt, "owned", false)
			stat := &capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID, Metadata: &capsulev1beta2.TenantStatusNamespaceMetadata{Labels: map[string]string{"managed": "old"}, Annotations: map[string]string{"managed": "old"}}, Conditions: meta.ConditionList{meta.NewReadyCondition(ns), meta.NewCordonedCondition(ns)}}
			tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{stat}
			if mode == "old-inventory-uid" {
				stat.UID = "previous-namespace"
			}
			if mode == "new-namespace" {
				tnt.Status.Spaces = nil
			}
			r, _ := namespaceCleanupFixture(t, tnt, ns)
			before := tnt.DeepCopy()
			failure := errors.New("read unavailable")
			var reads int
			r.provisioningReader = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				reads++
				if mode == "not-found" {
					return apierrors.NewNotFound(schema.GroupResource{Resource: "namespaces"}, key.Name)
				}
				if mode == "recreated-namespace" || mode == "reassigned-namespace" {
					err := c.Get(ctx, key, obj, opts...)
					actual := obj.(*corev1.Namespace)
					if mode == "recreated-namespace" {
						actual.UID = "replacement"
					} else {
						actual.OwnerReferences[0].UID = "other-tenant"
					}
					return err
				}
				return failure
			}})
			if mode == "cancelled" {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			}
			plan := prepareNamespaceRoleBindings(tnt)
			actual, err := r.provisionNamespace(ctx, logr.Discard(), ns, tnt, &plan)
			stale := mode == "not-found" || mode == "recreated-namespace" || mode == "reassigned-namespace"
			if stale {
				require.NoError(t, err)
				require.Nil(t, actual)
			} else {
				require.Error(t, err)
				if mode == "cancelled" {
					require.ErrorIs(t, err, context.Canceled)
					require.Zero(t, reads)
				} else {
					require.ErrorIs(t, err, failure)
					require.Equal(t, 1, reads)
				}
				require.NotNil(t, actual)
				require.Equal(t, ns.UID, actual.UID)
				if mode == "old-inventory-uid" || mode == "new-namespace" {
					require.Nil(t, actual.Metadata)
				} else {
					require.Equal(t, stat.Metadata, actual.Metadata)
					require.Equal(t, stat.Conditions.GetConditionByType(meta.CordonedCondition), actual.Conditions.GetConditionByType(meta.CordonedCondition))
					actual.Metadata.Labels["managed"] = "changed"
				}
				ready := actual.Conditions.GetConditionByType(meta.ReadyCondition)
				require.Equal(t, metav1.ConditionFalse, ready.Status)
				require.Equal(t, meta.FailedReason, ready.Reason)
			}
			require.Equal(t, before, tnt, "error handling must not mutate shared status")
		})
	}
}

func TestNamespaceMetadataCleanupRequiresMatchingUID(t *testing.T) {
	for _, recreated := range []bool{false, true} {
		t.Run(fmt.Sprintf("recreated=%v", recreated), func(t *testing.T) {
			tnt := provisioningTenant()
			ns := cleanupOwnedNamespace(tnt, "owned", false)
			ns.Labels["managed"], ns.Annotations = "user-value", map[string]string{"managed": "user-value"}
			inventory := &capsulev1beta2.TenantStatusNamespaceItem{Name: ns.Name, UID: ns.UID, Metadata: &capsulev1beta2.TenantStatusNamespaceMetadata{Labels: map[string]string{"managed": "old"}, Annotations: map[string]string{"managed": "old"}}}
			if recreated {
				inventory.UID = "previous-namespace"
			}
			tnt.Status.Spaces = []*capsulev1beta2.TenantStatusNamespaceItem{inventory}
			other := &capsulev1beta2.Tenant{Name: "tenant-b", UID: "tenant-b-uid"}
			foreign := cleanupOwnedNamespace(other, "foreign", false)
			foreign.Labels["managed"] = "foreign-value"
			r, _ := namespaceCleanupFixture(t, tnt, ns, other, foreign)
			require.NoError(t, r.reconcileActiveTenantNamespaces(t.Context(), logr.Discard(), tnt))
			actual := &corev1.Namespace{}
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(ns), actual))
			if recreated {
				require.Equal(t, "user-value", actual.Labels["managed"])
				require.Equal(t, "user-value", actual.Annotations["managed"])
			} else {
				require.NotContains(t, actual.Labels, "managed")
				require.NotContains(t, actual.Annotations, "managed")
			}
			require.Equal(t, ns.UID, tnt.Status.GetInstance(inventory).UID)
			require.NoError(t, r.Get(t.Context(), client.ObjectKeyFromObject(foreign), actual))
			require.Equal(t, "foreign-value", actual.Labels["managed"])
		})
	}
}
