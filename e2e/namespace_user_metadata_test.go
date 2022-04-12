//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("creating a Namespace with user-specified labels and annotations", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-user-metadata-forbidden",
			Annotations: map[string]string{
				capsulev1beta1.ForbiddenNamespaceLabelsAnnotation:            "foo,bar",
				capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation:      "^gatsby-.*$",
				capsulev1beta1.ForbiddenNamespaceAnnotationsAnnotation:       "foo,bar",
				capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation: "^gatsby-.*$",
			},
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "gatsby",
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

	It("should allow", func() {
		By("specifying non-forbidden labels", func() {
			ns := NewNamespace("namespace-user-metadata-allowed-labels")
			ns.SetLabels(map[string]string{"bim": "baz"})
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		})
		By("specifying non-forbidden annotations", func() {
			ns := NewNamespace("namespace-user-metadata-allowed-annotations")
			ns.SetAnnotations(map[string]string{"bim": "baz"})
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		})
	})

	It("should fail", func() {
		By("specifying forbidden labels using exact match", func() {
			ns := NewNamespace("namespace-user-metadata-forbidden-labels")
			ns.SetLabels(map[string]string{"foo": "bar"})
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
		By("specifying forbidden labels using regex match", func() {
			ns := NewNamespace("namespace-user-metadata-forbidden-labels")
			ns.SetLabels(map[string]string{"gatsby-foo": "bar"})
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using exact match", func() {
			ns := NewNamespace("namespace-user-metadata-forbidden-labels")
			ns.SetAnnotations(map[string]string{"foo": "bar"})
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
		By("specifying forbidden annotations using regex match", func() {
			ns := NewNamespace("namespace-user-metadata-forbidden-labels")
			ns.SetAnnotations(map[string]string{"gatsby-foo": "bar"})
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
		})
	})
})
