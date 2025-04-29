// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("creating several Namespaces for a Tenant", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capsule-labels",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "charlie",
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

	It("should contains the default Capsule label", func() {
		namespaces := []*v1.Namespace{
			NewNamespace(""),
			NewNamespace(""),
			NewNamespace(""),
		}
		for _, ns := range namespaces {
			NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
				ok, _ = HaveKeyWithValue("capsule.clastix.io/tenant", tnt.Name).Match(ns.Labels)
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		}
	})
})
