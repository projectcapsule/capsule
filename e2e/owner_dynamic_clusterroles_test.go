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

var _ = Describe("defining dynamic Tenant Owner Cluster Roles", Ordered, Label("tenant", "permissions", "owners", "rolebindings"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-dynamic-to-clusterroles",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Kind: "User",
							Name: "e2e-dynamic-to-clusterroles",
						},
						ClusterRoles: []string{"edit", "admin"},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "group:e2e-dynamic-to-clusterroles",
							Kind: "Group",
						},
						ClusterRoles: []string{"view"},
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

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})

	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("namespace should contains the dynamic rolebindings", func() {
		for _, ns := range []string{"dynamnic-roles-1", "dynamnic-roles-2", "dynamnic-roles-3"} {
			ns := NewNamespace(ns, map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			Eventually(CheckForOwnerRoleBindings(ns, tnt.Spec.Owners[0], map[string]bool{"edit": false, "admin": false}), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(CheckForOwnerRoleBindings(ns, tnt.Spec.Owners[1], map[string]bool{"view": false}), defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
