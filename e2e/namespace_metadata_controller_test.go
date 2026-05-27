// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", Ordered, Label("namespace", "metadata"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-metadata",
			Labels: map[string]string{
				"env": "e2e",
			},

			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "cap",
					Kind:       "dummy",
					Name:       "tenant-metadata",
					UID:        "tenant-metadata",
				},
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-tenant-metadata",
							Kind: "User",
						},
					},
				},
			},
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				AdditionalMetadata: &api.AdditionalMetadataSpec{
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
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)
	})

	It("should contain Namespace metadata after tenant update", func() {
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		By("checking labels", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
			Expect(ns.Labels).ShouldNot(HaveKeyWithValue("newlabel", "foobazbar"))

			Eventually(func() error {
				current := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current); err != nil {
					return err
				}

				if current.Spec.NamespaceOptions == nil {
					current.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{}
				}

				if current.Spec.NamespaceOptions.AdditionalMetadata == nil {
					current.Spec.NamespaceOptions.AdditionalMetadata = &api.AdditionalMetadataSpec{}
				}

				if current.Spec.NamespaceOptions.AdditionalMetadata.Labels == nil {
					current.Spec.NamespaceOptions.AdditionalMetadata.Labels = map[string]string{}
				}

				current.Spec.NamespaceOptions.AdditionalMetadata.Labels["newlabel"] = "foobazbar"

				return k8sClient.Update(context.TODO(), current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &corev1.Namespace{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, current)).Should(Succeed())
				g.Expect(current.Labels).Should(HaveKeyWithValue("newlabel", "foobazbar"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("checking annotations", func() {
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
			Expect(ns.Annotations).ShouldNot(HaveKeyWithValue("newannotation", "foobazbar"))

			Eventually(func() error {
				current := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current); err != nil {
					return err
				}

				if current.Spec.NamespaceOptions == nil {
					current.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{}
				}

				if current.Spec.NamespaceOptions.AdditionalMetadata == nil {
					current.Spec.NamespaceOptions.AdditionalMetadata = &api.AdditionalMetadataSpec{}
				}

				if current.Spec.NamespaceOptions.AdditionalMetadata.Annotations == nil {
					current.Spec.NamespaceOptions.AdditionalMetadata.Annotations = map[string]string{}
				}

				current.Spec.NamespaceOptions.AdditionalMetadata.Annotations["newannotation"] = "foobazbar"

				return k8sClient.Update(context.TODO(), current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &corev1.Namespace{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, current)).Should(Succeed())
				g.Expect(current.Annotations).Should(HaveKeyWithValue("newannotation", "foobazbar"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
	})
})
