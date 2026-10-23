// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

// The prefix is enforced at Tenant level, so the spec needs no change to the
// cluster-wide CapsuleConfiguration and runs in the parallel phase.
var _ = Describe(
	"creating a prefixed Namespace when one Tenant is matched more than once",
	Label("tenant", "prefix", "issue-2058"),
	func() {
		const (
			ownerGroup = "e2e-prefix-duplicate-group"
			username   = "e2e-prefix-duplicate-group-user"
		)

		tnt := &capsulev1beta2.Tenant{
			Name:   "e2e-prefix-duplicate-group",
			Labels: map[string]string{"env": "e2e"},
			Spec: capsulev1beta2.TenantSpec{
				ForceTenantPrefix: ptr.To(true),
				Owners: rbac.OwnerListSpec{{
					Kind: rbac.GroupOwner,
					Name: ownerGroup,
				}},
			},
		}

		JustBeforeEach(func() {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		})

		JustAfterEach(func() {
			EventuallyDeletion(tnt)
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
