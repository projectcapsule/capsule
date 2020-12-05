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
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace for a Tenant with additional metadata", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-metadata",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "gatsby",
				Kind: "User",
			},
			NamespacesMetadata: v1alpha1.AdditionalMetadata{
				AdditionalLabels: map[string]string{
					"k8s.io/custom-label":     "foo",
					"clastix.io/custom-label": "bar",
				},
				AdditionalAnnotations: map[string]string{
					"k8s.io/custom-annotation":     "bizz",
					"clastix.io/custom-annotation": "buzz",
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
	It("should contains additional Namespace metadata", func() {
		ns := NewNamespace("namespace-metadata")
		NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, podRecreationTimeoutInterval).Should(ContainElement(ns.GetName()))

		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
		By("checking additional labels", func() {
			for _, l := range tnt.Spec.NamespacesMetadata.AdditionalLabels {
				Expect(ns.Labels).Should(ContainElement(l))
			}
		})
		By("checking additional annotations", func() {
			for _, a := range tnt.Spec.NamespacesMetadata.AdditionalAnnotations {
				Expect(ns.Annotations).Should(ContainElement(a))
			}
		})
	})
})
