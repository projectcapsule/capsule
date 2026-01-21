// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package tenant_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/tenant"
)

func TestContextForTenantAndNamespace_BothNil(t *testing.T) {
	ctx := tenant.ContextForTenantAndNamespace(nil, nil)

	if ctx == nil {
		t.Fatalf("expected non-nil map")
	}
	if len(ctx) != 0 {
		t.Fatalf("expected empty map, got %v", ctx)
	}
}

func TestContextForTenantAndNamespace_OnlyTenant(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wind",
		},
	}

	ctx := tenant.ContextForTenantAndNamespace(tnt, nil)

	if got := ctx["tenant.name"]; got != "wind" {
		t.Fatalf("expected tenant.name=wind, got %q", got)
	}
	if _, ok := ctx["namespace"]; ok {
		t.Fatalf("did not expect namespace key to be set")
	}
	if len(ctx) != 1 {
		t.Fatalf("expected map size 1, got %d (%v)", len(ctx), ctx)
	}
}

func TestContextForTenantAndNamespace_OnlyNamespace(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wind-prod",
		},
	}

	ctx := tenant.ContextForTenantAndNamespace(nil, ns)

	if got := ctx["namespace"]; got != "wind-prod" {
		t.Fatalf("expected namespace=wind-prod, got %q", got)
	}
	if _, ok := ctx["tenant.name"]; ok {
		t.Fatalf("did not expect tenant.name key to be set")
	}
	if len(ctx) != 1 {
		t.Fatalf("expected map size 1, got %d (%v)", len(ctx), ctx)
	}
}

func TestContextForTenantAndNamespace_BothSet(t *testing.T) {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wind",
		},
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "wind-prod",
		},
	}

	ctx := tenant.ContextForTenantAndNamespace(tnt, ns)

	if got := ctx["tenant.name"]; got != "wind" {
		t.Fatalf("expected tenant.name=wind, got %q", got)
	}
	if got := ctx["namespace"]; got != "wind-prod" {
		t.Fatalf("expected namespace=wind-prod, got %q", got)
	}
	if len(ctx) != 2 {
		t.Fatalf("expected map size 2, got %d (%v)", len(ctx), ctx)
	}
}
