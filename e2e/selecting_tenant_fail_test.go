//+build e2e

/*
Copyright 2020 Clastix Labs.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace without a Tenant selector when user owns multiple Tenants", func() {
	t1 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenantone",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			IngressClasses:     v1alpha1.IngressClassesSpec{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			LimitRanges:        []corev1.LimitRangeSpec{},
			NamespaceQuota:     10,
			NodeSelector:       map[string]string{},
			ResourceQuota:      []corev1.ResourceQuotaSpec{},
		},
	}
	t2 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenanttwo",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			IngressClasses:     v1alpha1.IngressClassesSpec{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			LimitRanges:        []corev1.LimitRangeSpec{},
			NamespaceQuota:     10,
			NodeSelector:       map[string]string{},
			ResourceQuota:      []corev1.ResourceQuotaSpec{},
		},
	}
	t3 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenantthree",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "Group",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			IngressClasses:     v1alpha1.IngressClassesSpec{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			LimitRanges:        []corev1.LimitRangeSpec{},
			NamespaceQuota:     10,
			NodeSelector:       map[string]string{},
			ResourceQuota:      []corev1.ResourceQuotaSpec{},
		},
	}
	t4 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenantfour",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "john",
				Kind: "Group",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{},
			ServicesMetadata:   v1alpha1.AdditionalMetadata{},
			IngressClasses:     v1alpha1.IngressClassesSpec{},
			StorageClasses:     v1alpha1.StorageClassesSpec{},
			LimitRanges:        []corev1.LimitRangeSpec{},
			NamespaceQuota:     10,
			NodeSelector:       map[string]string{},
			ResourceQuota:      []corev1.ResourceQuotaSpec{},
		},
	}
	It("should fail", func() {
		ns := NewNamespace("fail-ns")
		By("user owns 2 tenants", func() {
			Expect(k8sClient.Create(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Create(context.TODO(), t2)).Should(Succeed())
			NamespaceCreationShouldNotSucceed(ns, t1, defaultTimeoutInterval)
			NamespaceCreationShouldNotSucceed(ns, t2, defaultTimeoutInterval)
			Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
		})
		By("group owns 2 tenants", func() {
			Expect(k8sClient.Create(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Create(context.TODO(), t4)).Should(Succeed())
			NamespaceCreationShouldNotSucceed(ns, t3, defaultTimeoutInterval)
			NamespaceCreationShouldNotSucceed(ns, t4, defaultTimeoutInterval)
			Expect(k8sClient.Delete(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t4)).Should(Succeed())
		})
		By("user and group owns 4 tenants", func() {
			t1.ResourceVersion, t2.ResourceVersion, t3.ResourceVersion, t4.ResourceVersion = "", "", "", ""
			Expect(k8sClient.Create(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Create(context.TODO(), t2)).Should(Succeed())
			Expect(k8sClient.Create(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Create(context.TODO(), t4)).Should(Succeed())
			NamespaceCreationShouldNotSucceed(ns, t1, defaultTimeoutInterval)
			NamespaceCreationShouldNotSucceed(ns, t2, defaultTimeoutInterval)
			NamespaceCreationShouldNotSucceed(ns, t3, defaultTimeoutInterval)
			NamespaceCreationShouldNotSucceed(ns, t4, defaultTimeoutInterval)
			Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t3)).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), t4)).Should(Succeed())
		})
	})

})
