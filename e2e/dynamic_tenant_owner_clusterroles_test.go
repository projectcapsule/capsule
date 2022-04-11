//go:build e2e
// +build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("defining dynamic Tenant Owner Cluster Roles", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "dynamic-tenant-owner-clusterroles",
			Annotations: map[string]string{
				"clusterrolenames.capsule.clastix.io/user.michonne": "editor,manager",
				"clusterrolenames.capsule.clastix.io/group.kingdom": "readonly",
			},
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "michonne",
					Kind: "User",
				},
				{
					Name: "kingdom",
					Kind: "Group",
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
