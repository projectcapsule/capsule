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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clastix/capsule/api/v1alpha1"
)

var _ = Describe("creating a Namespace with an additional Role Binding", func() {
	tnt := &v1alpha1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "additional-role-binding",
		},
		Spec: v1alpha1.TenantSpec{
			Owner: v1alpha1.OwnerSpec{
				Name: "dale",
				Kind: "User",
			},
			AdditionalRoleBindings: []v1alpha1.AdditionalRoleBindings{
				{
					ClusterRoleName: "crds-rolebinding",
					Subjects: []rbacv1.Subject{
						{
							Kind:     "Group",
							APIGroup: "rbac.authorization.k8s.io",
							Name:     "system:authenticated",
						},
					},
				},
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

	It("should be assigned to each Namespace", func() {
		for _, ns := range []string{"rb-1", "rb-2", "rb-3"} {
			ns := NewNamespace(ns)
			NamespaceCreation(ns, tnt, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))

			var rb *rbacv1.RoleBinding

			Eventually(func() (err error) {
				cs := ownerClient(tnt)
				rb, err = cs.RbacV1().RoleBindings(ns.Name).Get(context.Background(), fmt.Sprintf("capsule-%s-%s", tnt.Name, "crds-rolebinding"), metav1.GetOptions{})
				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(rb.RoleRef.Name).Should(Equal(tnt.Spec.AdditionalRoleBindings[0].ClusterRoleName))
			Expect(rb.Subjects).Should(Equal(tnt.Spec.AdditionalRoleBindings[0].Subjects))
		}
	})
})
