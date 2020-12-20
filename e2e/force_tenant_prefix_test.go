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

var _ = Describe("creating a Namespace with --force-tenant-name flag", func() {
	t1 := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "awesome",
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
			Name: "awesome-tenant",
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
			t1.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t1)
		}).Should(Succeed())
		EventuallyCreation(func() error {
			t2.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), t2)
		}).Should(Succeed())
		ModifyCapsuleManagerPodArgs(append(defaulManagerPodArgs, []string{"--force-tenant-prefix"}...))
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), t1)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), t2)).Should(Succeed())
		ModifyCapsuleManagerPodArgs(defaulManagerPodArgs)
	})

	It("should fail when non using prefix", func() {
		ns := NewNamespace("awesome")
		NamespaceCreation(ns, t1, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed using prefix", func() {
		ns := NewNamespace("awesome-namespace")
		NamespaceCreation(ns, t1, defaultTimeoutInterval).Should(Succeed())
	})

	It("should succeed and assigned according to closest match", func() {
		ns1 := NewNamespace("awesome-tenant")
		ns2 := NewNamespace("awesome-tenant-namespace")

		NamespaceCreation(ns1, t1, defaultTimeoutInterval).Should(Succeed())
		NamespaceCreation(ns2, t2, defaultTimeoutInterval).Should(Succeed())

		TenantNamespaceList(t1, defaultTimeoutInterval).Should(ContainElement(ns1.GetName()))
		TenantNamespaceList(t2, defaultTimeoutInterval).Should(ContainElement(ns2.GetName()))
	})
})
