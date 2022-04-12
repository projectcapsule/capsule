//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-metadata",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "gatsby",
					Kind: "User",
				},
			},
			NamespaceOptions: &capsulev1beta1.NamespaceOptions{
				AdditionalMetadata: &capsulev1beta1.AdditionalMetadataSpec{
					Labels: map[string]string{
						"k8s.io/custom-label":     "foo",
						"clastix.io/custom-label": "bar",
					},
					Annotations: map[string]string{
						"k8s.io/custom-annotation":     "bizz",
						"clastix.io/custom-annotation": "buzz",
					},
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
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

		By("checking additional labels", func() {
			Eventually(func() (ok bool) {
				Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadata.Labels {
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
				for k, v := range tnt.Spec.NamespaceOptions.AdditionalMetadata.Annotations {
					if ok, _ = HaveKeyWithValue(k, v).Match(ns.Annotations); !ok {
						return
					}
				}
				return
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})
})
