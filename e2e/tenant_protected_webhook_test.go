//go:build e2e

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

var _ = Describe("Deleting a tenant with protected annotation", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "protected-tenant",
			Annotations: map[string]string{
				capsulev1beta1.ProtectedTenantAnnotation: "",
			},
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
				},
			},
		},
	}

	It("should fail", func() {
		Expect(k8sClient.Create(context.TODO(), tnt)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), tnt)).ShouldNot(Succeed())
	})
})
