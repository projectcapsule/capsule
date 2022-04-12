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

var _ = Describe("creating a Namespace creation with no Tenant assigned", func() {
	It("should fail", func() {
		tnt := &capsulev1beta1.Tenant{
			Spec: capsulev1beta1.TenantSpec{
				Owners: capsulev1beta1.OwnerListSpec{
					{
						Name: "missing",
						Kind: "User",
					},
				},
			},
		}
		ns := NewNamespace("no-namespace")
		cs := ownerClient(tnt.Spec.Owners[0])
		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).ShouldNot(Succeed())
	})
})
