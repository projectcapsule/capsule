//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating several Namespaces for a Tenant", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capsule-labels",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "charlie",
				Kind: "User",
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

	It("should contains the default Capsule label", func() {
		namespaces := []*v1.Namespace{
			NewNamespace("first-capsule-ns"),
			NewNamespace("second-capsule-ns"),
			NewNamespace("third-capsule-ns"),
		}
		for _, ns := range namespaces {
			NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
				ok, _ = HaveKeyWithValue("capsule.clastix.io/tenant", tnt.Name).Match(ns.Labels)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		}
	})
})
