// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

var _ = Describe("creating a Namespace with Tenant selector when user owns multiple tenants", Label("tenant", "assignment"), func() {
	t1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-one",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					UserSpec: api.UserSpec{
						Name: "john",
						Kind: "User",
					},
				},
			},
		},
	}
	t2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-two",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					UserSpec: api.UserSpec{
						Name: "john",
						Kind: "User",
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			t1.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			t2.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t2)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
	})

	It("should be assigned to the selected Tenant", func() {
		ns := NewNamespace("")
		By("assigning to the Namespace the Capsule Tenant label", func() {
			ns.Labels = map[string]string{
				meta.TenantLabel: t2.Name,
			}
		})
		NamespaceCreation(ns, t2.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(t2, ns)
	})

	It("prevent reassignment via labels from owners", func() {
		ns := NewNamespace("")
		By("assigning to the Namespace the Capsule Tenant label", func() {
			ns.Labels = map[string]string{
				meta.TenantLabel: t1.Name,
			}

			NamespaceCreation(ns, t1.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, ns)
		})

		By("assigning to the Namespace the Capsule Tenant label (Attempt Label Patch)", func() {
			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]string{
						meta.TenantLabel: t2.Name,
					},
				},
			}

			err := PatchNamespace(ns, ownerClient(t2.Spec.Owners[0].UserSpec), patch)
			Expect(err).NotTo(HaveOccurred())

			new := &corev1.Namespace{}
			k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, new)

			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, new)
		})

		By("assigning to the Namespace the Capsule Tenant label (Attempt Ownerreference Patch)", func() {
			ref, err := GetTenantOwnerReferenceAsPatch(t2)
			Expect(err).NotTo(HaveOccurred())

			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]string{
						meta.TenantLabel: t1.Name,
					},
					"ownerReferences": []map[string]interface{}{ref},
				},
			}

			err = PatchNamespace(ns, ownerClient(t2.Spec.Owners[0].UserSpec), patch)
			Expect(err).NotTo(HaveOccurred())

			new := &corev1.Namespace{}
			k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, new)

			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, new)
		})

		By("assigning to the Namespace the Capsule Tenant label (Attempt Ownerreference Patch) - Without Label", func() {
			ref, err := GetTenantOwnerReferenceAsPatch(t2)
			Expect(err).NotTo(HaveOccurred())

			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels":          map[string]string{},
					"ownerReferences": []map[string]interface{}{ref},
				},
			}

			err = PatchNamespace(ns, ownerClient(t2.Spec.Owners[0].UserSpec), patch)
			Expect(err).NotTo(HaveOccurred())

			new := &corev1.Namespace{}
			k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, new)

			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, ns)
		})

		By("assigning to the Namespace the Capsule Tenant label (Empty Ownerreferences)", func() {
			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]string{
						meta.TenantLabel: t2.Name,
					},
					"ownerReferences": []string{},
				},
			}

			err := PatchNamespace(ns, ownerClient(t2.Spec.Owners[0].UserSpec), patch)
			Expect(err).NotTo(HaveOccurred())

			new := &corev1.Namespace{}
			k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, new)

			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, new)
		})

		By("assigning to the Namespace the Capsule Tenant label (Empty Ownerreferences) - Without Label", func() {
			ns.Labels = map[string]string{}

			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels":          map[string]string{},
					"ownerReferences": []string{},
				},
			}

			err := PatchNamespace(ns, ownerClient(t2.Spec.Owners[0].UserSpec), patch)
			Expect(err).NotTo(HaveOccurred())

			new := &corev1.Namespace{}
			k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, new)

			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, ns)
		})

		By("assigning to the Namespace the Capsule Tenant label (2nd Tenant Label + Ownerreference)", func() {
			ref, err := GetTenantOwnerReferenceAsPatch(t2)
			Expect(err).NotTo(HaveOccurred())

			patch := map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]string{
						meta.TenantLabel: t2.Name,
					},
					"ownerReferences": []map[string]interface{}{ref},
				},
			}

			err = PatchNamespace(ns, ownerClient(t2.Spec.Owners[0].UserSpec), patch)
			Expect(err).NotTo(HaveOccurred())

			new := &corev1.Namespace{}
			k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, new)

			TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
			NamespaceIsPartOfTenant(t1, new)
		})
	})
})
