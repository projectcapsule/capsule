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

var _ = Describe("creating a Namespaces as different type of Tenant owners", Label("namespace", "permissions", "owners"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-assigned",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "alice",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "bob",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "system:serviceaccount:new-namespace-sa:default",
							Kind: "ServiceAccount",
						},
					},
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
		EventuallyDeletion(tnt)
	})

	It("should be available in Tenant namespaces list and RoleBindings should be present when created", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		for _, owner := range tnt.Spec.Owners {
			Eventually(CheckForOwnerRoleBindings(ns, owner, nil), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
	It("should be available in Tenant namespaces list and RoleBindings should present when created as Group", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[1].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		for _, owner := range tnt.Spec.Owners {
			Eventually(CheckForOwnerRoleBindings(ns, owner, nil), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		c := impersonationClient(tnt.Spec.Owners[1].UserSpec.Name, withDefaultGroups(nil))

		err := c.Delete(context.TODO(), ns)
		Expect(err).ToNot(HaveOccurred())

	})
	It("should be available in Tenant namespaces list and RoleBindings should present when created as ServiceAccount", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[2].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		for _, owner := range tnt.Spec.Owners {
			Eventually(CheckForOwnerRoleBindings(ns, owner, nil), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
