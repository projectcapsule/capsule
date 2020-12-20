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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace with Tenant selector when user owns multiple tenants", func() {
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

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), t1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), t2)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
	})

	It("should be assigned to the selected Tenant", func() {
		ns := NewNamespace("tenant-2-ns")
		By("assigning to the Namespace the Capsule Tenant label", func() {
			l, err := v1alpha1.GetTypeLabel(&v1alpha1.Tenant{})
			Expect(err).ToNot(HaveOccurred())
			ns.Labels = map[string]string{
				l: t2.Name,
			}
		})
		NamespaceCreation(ns, t2, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(t2, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})
})
