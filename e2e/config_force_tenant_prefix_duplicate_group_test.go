// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe(
	"creating a prefixed Namespace when one Tenant is matched more than once",
	Ordered,
	Label("config", "tenant", "prefix", "issue-2058"),
	func() {
		const (
			ownerGroup = "e2e-prefix-duplicate-group"
			username   = "e2e-prefix-duplicate-group-user"
		)

		tnt := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "e2e-prefix-duplicate-group",
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Kind: rbac.GroupOwner,
							Name: ownerGroup,
						},
					},
				}},
			},
		}

		JustBeforeEach(func() {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.ForceTenantPrefix = true
			})
		})

		JustAfterEach(func() {
			EventuallyDeletion(tnt)

			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.ForceTenantPrefix = false
			})
		})

		It("assigns the Namespace when the owning group is repeated in the request", func() {
			namespace := NewNamespace(tnt.GetName() + "-namespace")
			clientset := impersonationClientSet(
				username,
				withDefaultGroups([]string{ownerGroup, ownerGroup}),
			)

			Eventually(func() error {
				_, err := clientset.CoreV1().
					Namespaces().
					Create(context.TODO(), namespace, metav1.CreateOptions{})

				return err
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			NamespaceIsPartOfTenant(tnt, namespace).Should(Succeed())
		})
	},
)
