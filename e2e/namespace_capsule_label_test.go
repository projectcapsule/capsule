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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating several Namespaces for a Tenant", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "capsule-labels",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "charlie",
				Kind: "User",
			},
		},
	}
	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), tnt.DeepCopy())
		}).Should(Succeed())
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})
	It("should contains the default Capsule label", func() {
		namespaces := []*v1.Namespace{
			NewNamespace("first-capsule-ns"),
			NewNamespace("second-capsule-ns"),
			NewNamespace("third-capsule-ns"),
		}
		for _, ns := range namespaces {
			NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
			Expect(ns.Labels).Should(HaveKeyWithValue("capsule.clastix.io/tenant", tnt.Name))
		}
	})
})
