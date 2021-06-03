//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace creation with no Tenant assigned", func() {
	It("should fail", func() {
		tnt := &v1alpha1.Tenant{
			Spec: v1alpha1.TenantSpec{
				Owner: v1alpha1.OwnerSpec{
					Name: "missing",
					Kind: "User",
				},
			},
		}
		ns := NewNamespace("no-namespace")
		cs := ownerClient(tnt)
		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).ShouldNot(Succeed())
	})
})
