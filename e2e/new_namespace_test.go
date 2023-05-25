//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("creating a Namespaces as different type of Tenant owners", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-assigned",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "alice",
					Kind: "User",
				},
				{
					Name: "bob",
					Kind: "Group",
				},
				{
					Name: "system:serviceaccount:new-namespace-sa:default",
					Kind: "ServiceAccount",
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should be available in Tenant namespaces list and RoleBindings should be present when created", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		for _, owner := range tnt.Spec.Owners {
			Eventually(CheckForOwnerRoleBindings(ns, owner, nil), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
	It("should be available in Tenant namespaces list and RoleBindings should present when created as Group", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[1], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		for _, owner := range tnt.Spec.Owners {
			Eventually(CheckForOwnerRoleBindings(ns, owner, nil), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
	It("should be available in Tenant namespaces list and RoleBindings should present when created as ServiceAccount", func() {
		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[2], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		for _, owner := range tnt.Spec.Owners {
			Eventually(CheckForOwnerRoleBindings(ns, owner, nil), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
