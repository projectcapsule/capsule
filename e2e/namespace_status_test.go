// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating namespace with status lifecycle", Ordered, Label("namespace", "status"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-status",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-tenant-status",
							Kind: "User",
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

		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("verify namespace lifecycle (functionality)", func() {
		ns1 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		By("creating first namespace", func() {
			NamespaceCreation(ns1, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns1).Should(Succeed())
			TenantNamespaceReady(tnt, ns1, 1)
		})

		ns2 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		By("creating second namespace", func() {
			NamespaceCreation(ns2, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns2).Should(Succeed())
			TenantNamespaceReady(tnt, ns2, 2)
		})

		By("removing first namespace", func() {
			cs := impersonationClient(tnt.Spec.Owners[0].UserSpec.Name, withDefaultGroups(nil))
			Expect(cs.Delete(context.TODO(), ns1)).Should(Succeed())

			Eventually(func(g Gomega) {
				t := &capsulev1beta2.Tenant{}

				err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tnt.GetName()},
					t,
				)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(t.Status.Size).To(Equal(uint(1)))

				instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns1.GetName(),
					UID:  ns1.GetUID(),
				})
				g.Expect(instance).To(BeNil(), "Namespace instance should be nil")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("removing second namespace", func() {
			Expect(k8sClient.Delete(context.TODO(), ns2)).Should(Succeed())

			Eventually(func(g Gomega) {
				t := &capsulev1beta2.Tenant{}

				err := k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Name: tnt.GetName()},
					t,
				)
				g.Expect(err).ToNot(HaveOccurred())

				instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns2.GetName(),
					UID:  ns2.GetUID(),
				})
				g.Expect(instance).To(BeNil(), "Namespace instance should be nil")

				g.Expect(t.Status.Size).To(Equal(uint(0)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		})
	})
})
