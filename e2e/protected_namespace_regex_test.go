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

var _ = Describe("creating a Namespace with a protected Namespace regex enabled", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-protected-namespace",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "alice",
				Kind: "User",
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *v1alpha1.CapsuleConfiguration) {
			configuration.Spec.ProtectedNamespaceRegexpString = `^.*[-.]system$`
		})

		ns := NewNamespace("test-ok")

		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	It("should fail using a value non matching the regex", func() {
		ns := NewNamespace("test-system")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).ShouldNot(Succeed())

		ModifyCapsuleConfigurationOpts(func(configuration *v1alpha1.CapsuleConfiguration) {
			configuration.Spec.ProtectedNamespaceRegexpString = ""
		})
	})
})
