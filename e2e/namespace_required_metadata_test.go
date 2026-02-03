// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("creating a Namespace for a Tenant with required metadata", Label("namespace", "metadata", "me"), func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-metadata-required",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "gatsby",
							Kind: "User",
						},
					},
				},
			},
			NamespaceOptions: &capsulev1beta2.NamespaceOptions{
				RequiredMetadata: &capsulev1beta2.RequiredMetadata{
					Labels: map[string]string{
						"environment": "^(prod|test|dev)$",
					},
					Annotations: map[string]string{
						"example.corp/cost-center": "^INV-[0-9]{4}$",
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

	It("should contain required Namespace metadata", func() {
		By("creating without required label", func() {
			ns := NewNamespace("")

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).ShouldNot(ContainElement(ns.GetName()))
		})

		By("creating with required label, without annotation", func() {
			ns := NewNamespace("", map[string]string{
				"environment": "prod",
			})
			ns.SetAnnotations(map[string]string{})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).ShouldNot(ContainElement(ns.GetName()))
		})

		By("creating with required label and annotation", func() {
			ns := NewNamespace("", map[string]string{
				"environment": "prod",
			})
			ns.SetAnnotations(map[string]string{
				"example.corp/cost-center": "INV-1234",
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
			ns.SetLabels(map[string]string{
				"environment": "UAT",
			})

			ns.SetAnnotations(map[string]string{
				"example.corp/cost-center": "INV-1",
			})

			c := impersonationClient(tnt.Spec.Owners[0].UserSpec.Name, withDefaultGroups(nil))

			err := c.Update(context.TODO(), ns)
			Expect(err).ShouldNot(Succeed(), "expected failure")

		})

		By("creating with required label (wrong value) and annotation", func() {
			ns := NewNamespace("", map[string]string{
				"environment": "UAT",
			})
			ns.SetAnnotations(map[string]string{
				"example.corp/cost-center": "INV-1234",
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).ShouldNot(ContainElement(ns.GetName()))
		})

		By("creating with required label and annotation (wrong value)", func() {
			ns := NewNamespace("", map[string]string{
				"environment": "prod",
			})
			ns.SetAnnotations(map[string]string{
				"example.corp/cost-center": "INV-1",
			})

			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).ShouldNot(ContainElement(ns.GetName()))
		})

	})
})
