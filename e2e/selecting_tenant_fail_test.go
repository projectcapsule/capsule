//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
)

var _ = Describe("creating a Namespace without a Tenant selector when user owns multiple Tenants", func() {
	t1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-one",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
				},
			},
		},
	}
	t2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-two",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "User",
				},
			},
		},
	}
	t3 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-three",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "Group",
				},
			},
		},
	}
	t4 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-four",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "john",
					Kind: "Group",
				},
			},
		},
	}

	It("should fail", func() {
		ns := NewNamespace("")
		By("user owns 2 tenants", func() {
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t1) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t2) }).Should(Succeed())
			NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t2.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
		})
		By("group owns 2 tenants", func() {
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t3) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t4) }).Should(Succeed())
			NamespaceCreation(ns, t3.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t4.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t4)).Should(Succeed())
		})
		By("user and group owns 4 tenants", func() {
			t1.ResourceVersion, t2.ResourceVersion, t3.ResourceVersion, t4.ResourceVersion = "", "", "", ""
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t1) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t2) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t3) }).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t4) }).Should(Succeed())
			NamespaceCreation(ns, t1.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t2.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t3.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t4.Spec.Owners[0], defaultTimeoutInterval).ShouldNot(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t4)).Should(Succeed())
		})
	})
})
