//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace without a Tenant selector when user owns multiple Tenants", func() {
	t1 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-one",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "User",
			},
		},
	}
	t2 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-two",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "User",
			},
		},
	}
	t3 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-three",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "Group",
			},
		},
	}
	t4 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-four",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "Group",
			},
		},
	}

	It("should fail", func() {
		ns := NewNamespace("fail-ns")
		By("user owns 2 tenants", func() {
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t1)}).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t2)}).Should(Succeed())
			NamespaceCreation(ns, t1, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t2, defaultTimeoutInterval).ShouldNot(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
		})
		By("group owns 2 tenants", func() {
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t3)}).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t4)}).Should(Succeed())
			NamespaceCreation(ns, t3, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t4, defaultTimeoutInterval).ShouldNot(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t4)).Should(Succeed())
		})
		By("user and group owns 4 tenants", func() {
			t1.ResourceVersion, t2.ResourceVersion, t3.ResourceVersion, t4.ResourceVersion = "", "", "", ""
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t1)}).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t2)}).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t3)}).Should(Succeed())
			EventuallyCreation(func() error { return k8sClient.Create(context.TODO(), t4)}).Should(Succeed())
			NamespaceCreation(ns, t1, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t2, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t3, defaultTimeoutInterval).ShouldNot(Succeed())
			NamespaceCreation(ns, t4, defaultTimeoutInterval).ShouldNot(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t4)).Should(Succeed())
		})
	})
})
