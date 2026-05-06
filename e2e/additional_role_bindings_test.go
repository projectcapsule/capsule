// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace with an additional Role Binding", Label("tenant", "rolebindings"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "additional-role-binding",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "dale",
							Kind: "User",
						},
					},
				},
			},
			AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "crds-rolebinding",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: rbacv1.GroupName,
							Name:     "system:authenticated",
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

	It("should be assigned to each Namespace", func() {

		t := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).Should(Succeed())

		VerifyTenantRoleBindings(t)
	})
})
