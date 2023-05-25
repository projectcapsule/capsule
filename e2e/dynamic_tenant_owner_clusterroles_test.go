//go:build e2e
// +build e2e

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

var _ = Describe("defining dynamic Tenant Owner Cluster Roles", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dynamic-tenant-owner-clusterroles",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Kind:         "User",
					Name:         "michonne",
					ClusterRoles: []string{"editor", "manager"},
				},
				{
					Name:         "kingdom",
					Kind:         "Group",
					ClusterRoles: []string{"readonly"},
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

	It("namespace should contains the dynamic rolebindings", func() {
		for _, ns := range []string{"dynamnic-roles-1", "dynamnic-roles-2", "dynamnic-roles-3"} {
			ns := NewNamespace(ns)
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			Eventually(CheckForOwnerRoleBindings(ns, tnt.Spec.Owners[0], map[string]bool{"editor": false, "manager": false}), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(CheckForOwnerRoleBindings(ns, tnt.Spec.Owners[1], map[string]bool{"readonly": false}), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
