//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-metadata",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "gatsby",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{
				AdditionalLabels: map[string]string{
					"k8s.io/custom-label":     "foo",
					"clastix.io/custom-label": "bar",
				},
				AdditionalAnnotations: map[string]string{
					"k8s.io/custom-annotation":     "bizz",
					"clastix.io/custom-annotation": "buzz",
				},
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

	It("should contain additional Namespace metadata", func() {
		ns := NewNamespace("namespace-metadata")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
				for k, v := range tnt.Spec.NamespacesMetadata.AdditionalLabels {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Labels); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
		By("checking additional annotations", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
				for k, v := range tnt.Spec.NamespacesMetadata.AdditionalAnnotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})
})
