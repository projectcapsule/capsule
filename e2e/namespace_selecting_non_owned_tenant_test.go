// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("creating a Namespace trying to select a third Tenant", Ordered, Label("namespace", "tenant", "assignment"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-non-owned",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-tenant-non-owned",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should fail", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.Name,
		})

		cs := ownerClient(rbac.UserSpec{Name: "e2e-tenant-non-owned-fail", Kind: "User"})
		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
	})
})
