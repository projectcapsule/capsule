//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("creating a Namespace in over-quota of three", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "over-quota-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "bob",
					Kind: "User",
				},
			},
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				Quota: pointer.Int32(3),
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should fail", func() {
		By("creating three Namespaces", func() {
			for _, name := range []string{"bob-dev", "bob-staging", "bob-production"} {
				ns := NewNamespace(name)
				NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
				TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			}
		})

		ns := NewNamespace("")
		cs := ownerClient(tnt.Spec.Owners[0])
		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).ShouldNot(Succeed())
	})
})
