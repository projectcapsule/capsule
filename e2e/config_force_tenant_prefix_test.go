// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace with Tenant name prefix enforcement", Ordered, Label("tenant", "config", "prefix"), func() {
	t1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-prefix",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-prefix",
							Kind: "User",
						},
					},
				},
			},
		},
	}
	t2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-prefix-tenant",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-prefix",
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

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = true
		})
	})
	JustAfterEach(func() {
		EventuallyDeletion(t1)
		EventuallyDeletion(t2)

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.ForceTenantPrefix = false
		})
	})

	It("should fail when non using prefix (Single Tenant)", func() {
		EventuallyDeletion(t2)
		ns := NewNamespace("custom")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should fail when non using prefix (Single Tenant)", func() {
		EventuallyDeletion(t2)
		ns := NewNamespace("e2e-prefix")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed using prefix (Single Tenant)", func() {
		EventuallyDeletion(t2)

		ns := NewNamespace("e2e-prefix-namespace")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
	})

	It("should fail when non using prefix", func() {
		ns := NewNamespace("e2e-prefix")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed using prefix", func() {
		ns := NewNamespace("e2e-prefix-namespace")
		NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
	})

	It("should succeed and assigned according to closest match", func() {
		ns1 := NewNamespace("e2e-prefix-tenant")
		ns2 := NewNamespace("e2e-prefix-tenant-namespace")

		NamespaceCreation(ns1, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t1, ns1).Should(Succeed())
		NamespaceCreation(ns2, t2.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t2, ns2).Should(Succeed())
	})
})
