// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

func createResourcePermitTestNamespace(ctx context.Context) *corev1.Namespace {
	namespace := NewNamespace("e2e-resourcepermit-" + rand.String(10))
	// Namespace termination permits cleanup in every lifecycle phase. Deleting
	// an initializing or active ResourcePermit directly is rejected by admission.
	DeferCleanup(func() {
		ForceDeleteNamespace(ctx, namespace.Name)
	})
	NamespaceCreationAdmin(namespace, defaultTimeoutInterval).Should(Succeed())

	return namespace
}

func createResourcePermitTenantNamespace(ctx context.Context) *corev1.Namespace {
	name := "e2e-resourcepermit-" + rand.String(10)
	tenant := &capsulev1beta2.Tenant{
		Name: name, Labels: map[string]string{"env": "e2e"},
		Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{
			UserSpec: rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner},
		}}},
	}
	Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
	DeferCleanup(func() { EventuallyDeletion(tenant) })
	TenantReadyTrue(tenant)

	namespace := NewNamespace(name, map[string]string{meta.TenantLabel: name})
	DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
	NamespaceCreation(namespace, tenant.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
	TenantNamespaceReady(tenant, namespace, 1)

	return namespace
}
