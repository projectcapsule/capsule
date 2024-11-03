//go:build e2e

// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("creating a Namespace with Tenant name prefix enforcement at Tenant scope", func() {
	t1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "awesome",
		},
		Spec: capsulev1beta2.TenantSpec{
			ForceTenantPrefix: &[]bool{true}[0],
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
				},
			},
		},
	}
	t2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "awesome-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			ForceTenantPrefix: &[]bool{false}[0],
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			t1.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			t2.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t2)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = false
		})
	})

	It("should fail when not using prefix, with tenant label for a tenant with ForceTenantPrefix true and global ForceTenantPrefix false", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = false
		})
		labels := map[string]string{
			"capsule.clastix.io/tenant": t1.GetName(),
		}
		ns := NewNamespace("awesome", labels)
		NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should fail using prefix without capsule.clastix.io/tenant label, where the user owns more than one Tenant, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix false", func() {
		ns := NewNamespace("awesome-namespace")
		NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should fail using prefix without capsule.clastix.io/tenant label, where the user owns more than one Tenant, for a tenant with ForceTenantPrefix false and global ForceTenantPrefix true", func() {
		ns := NewNamespace("awesome-namespace")
		NamespaceCreation(ns, t2.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed and be assigned with prefix and label, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix false", func() {
		labels := map[string]string{
			"capsule.clastix.io/tenant": t1.GetName(),
		}
		ns := NewNamespace("awesome-tenant", labels)
		NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	It("should fail when not using prefix, with tenant label for a tenant with ForceTenantPrefix true and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})
		labels := map[string]string{
			"capsule.clastix.io/tenant": t1.GetName(),
		}
		ns := NewNamespace("awesome", labels)
		NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed and be assigned with prefix and label, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})
		labels := map[string]string{
			"capsule.clastix.io/tenant": t1.GetName(),
		}
		ns := NewNamespace("awesome-tenant", labels)
		NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())

		TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	It("should fail using prefix without capsule.clastix.io/tenant label, for a tenant with ForceTenantPrefix true and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})
		ns := NewNamespace("awesome-namespace")
		NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed when not using prefix, with tenant label for a tenant with ForceTenantPrefix false and global ForceTenantPrefix false", func() {
		labels := map[string]string{
			"capsule.clastix.io/tenant": t2.GetName(),
		}
		ns := NewNamespace("awesome", labels)
		NamespaceCreation(ns, t2.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
	})

	It("should succeed when not using prefix, with tenant label for a tenant with ForceTenantPrefix false and global ForceTenantPrefix true", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})
		labels := map[string]string{
			"capsule.clastix.io/tenant": t2.GetName(),
		}
		ns := NewNamespace("awesome", labels)
		NamespaceCreation(ns, t2.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
	})
})
