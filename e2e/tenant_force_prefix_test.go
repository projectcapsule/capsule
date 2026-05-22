// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace with Tenant name prefix enforcement at Tenant scope", Ordered, Label("config", "tenant", "prefix"), func() {
	t1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-force-prefix",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			ForceTenantPrefix: &[]bool{true}[0],
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "john",
							Kind: "User",
						},
					},
				},
			},
		},
	}
	t2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-force-prefix-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			ForceTenantPrefix: &[]bool{false}[0],
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "john",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			t1.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t1)
		}).Should(Succeed())

		TenantReady(t1, metav1.ConditionTrue, defaultTimeoutInterval)

		EventuallyCreation(func() error {
			t2.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t2)
		}).Should(Succeed())

		TenantReady(t2, metav1.ConditionTrue, defaultTimeoutInterval)

	})
	JustAfterEach(func() {
		EventuallyDeletion(t1)
		EventuallyDeletion(t2)

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = false
		})
	})

	It("should fail when not using prefix, with tenant label for a tenant with ForceTenantPrefix true and global ForceTenantPrefix false", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = false
		})

		ns := NewNamespace("e2e-tenant-force-prefix", map[string]string{
			meta.TenantLabel: t1.GetName(),
		})
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsNotPartOfTenant(t1, ns).Should(Succeed())
	})

	It("should fail using prefix without capsule.clastix.io/tenant label, where the user owns more than one Tenant, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix false", func() {
		ns := NewNamespace("e2e-tenant-force-prefix-namespace")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsNotPartOfTenant(t1, ns).Should(Succeed())
	})

	It("should fail using prefix without capsule.clastix.io/tenant label, where the user owns more than one Tenant, for a tenant with ForceTenantPrefix false and global ForceTenantPrefix true", func() {
		ns := NewNamespace("e2e-tenant-force-prefix-namespace")
		NamespaceCreation(ns, t2.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsNotPartOfTenant(t2, ns).Should(Succeed())
	})

	It("should succeed and be assigned with prefix and label, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix false", func() {
		ns := NewNamespace("e2e-tenant-force-prefix-tenant", map[string]string{
			meta.TenantLabel: t1.GetName(),
		})
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t1, ns).Should(Succeed())

	})

	It("should fail when not using prefix, with tenant label for a tenant with ForceTenantPrefix true and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})

		ns := NewNamespace("e2e-tenant-force-prefix", map[string]string{
			meta.TenantLabel: t1.GetName(),
		})
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsPartOfTenant(t1, ns).Should(Succeed())
	})

	It("should succeed and be assigned with prefix and label, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})

		ns := NewNamespace("e2e-tenant-force-prefix-tenant", map[string]string{
			meta.TenantLabel: t1.GetName(),
		})
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t1, ns).Should(Succeed())
	})

	It("should fail using prefix without capsule.clastix.io/tenant label, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})
		ns := NewNamespace("e2e-tenant-force-prefix-namespace")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsNotPartOfTenant(t1, ns).Should(Succeed())
	})

	It("should succeed when not using prefix, with tenant label for a tenant with ForceTenantPrefix false and global ForceTenantPrefix false", func() {
		ns := NewNamespace("e2e-tenant-force-prefix", map[string]string{
			meta.TenantLabel: t2.GetName(),
		})
		NamespaceCreation(ns, t2.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t2, ns).Should(Succeed())
	})

	It("should succeed when not using prefix, with tenant label for a tenant with ForceTenantPrefix false and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})

		ns := NewNamespace("e2e-tenant-force-prefix", map[string]string{
			meta.TenantLabel: t2.GetName(),
		})
		NamespaceCreation(ns, t2.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t2, ns).Should(Succeed())
	})
})
