//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("Deleting a tenant with protected annotation", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "protected-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			PreventDeletion: true,
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
				},
			},
		},
	}

	JustAfterEach(func() {
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, tnt)).Should(Succeed())
		tnt.Spec.PreventDeletion = false
		Expect(k8sClient.Update(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should fail", func() {
		Expect(k8sClient.Create(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), tnt)).ShouldNot(Succeed())
	})
})
