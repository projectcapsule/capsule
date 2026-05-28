// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace creation with no Tenant assigned", Ordered, Label("tenant", "permissions", "owners"), func() {
	It("should fail", func() {
		tnt := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-missing-user",
				Labels: map[string]string{
					"env": "e2e",
				},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						CoreOwnerSpec: rbac.CoreOwnerSpec{
							UserSpec: rbac.UserSpec{
								Name: "e2e-missing-user",
								Kind: "User",
							},
						},
					},
				},
			},
		}
		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		_, err := cs.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).ShouldNot(Succeed())
	})
})
