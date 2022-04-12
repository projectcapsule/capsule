//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("creating a Namespace with a protected Namespace regex enabled", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-protected-namespace",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "alice",
					Kind: "User",
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

	It("should succeed and be available in Tenant namespaces list", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.ProtectedNamespaceRegexpString = `^.*[-.]system$`
		})

		ns := NewNamespace("test-ok")

		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	It("should fail using a value non matching the regex", func() {
		ns := NewNamespace("test-system")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.ProtectedNamespaceRegexpString = ""
		})
	})
})
