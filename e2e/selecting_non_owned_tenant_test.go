//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace trying to select a third Tenant", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-non-owned",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "undefined",
				Kind: "User",
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
		var ns *corev1.Namespace

		By("assigning to the Namespace the Capsule Tenant label", func() {
			l, err := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
			Expect(err).ToNot(HaveOccurred())

			ns := NewNamespace("tenant-non-owned-ns")
			ns.SetLabels(map[string]string{
				l: tnt.Name,
			})
		})

		cs := ownerClient(&v1alpha1.Tenant{Spec: v1alpha1.TenantSpec{Owner: v1alpha1.OwnerSpec{Name: "dale", Kind: "User"}}})
		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
	})
})
