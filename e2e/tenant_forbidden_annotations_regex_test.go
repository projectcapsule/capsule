// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a tenant with various forbidden regexes", Ordered, Label("tenant", "metadata", "forbidden"), func() {
	successRegexes := []string{
		"",
		"(.*gitops|.*nsm)",
	}
	for i, annotationValue := range successRegexes {
		It("should succeed using a valid regex on the annotation", func() {
			tnt := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "e2e-namespace-regex-" + strconv.Itoa(i),
					Labels: map[string]string{
						"env": "e2e",
					},
				},
				Spec: capsulev1beta2.TenantSpec{
					Owners: rbac.OwnerListSpec{
						{
							CoreOwnerSpec: rbac.CoreOwnerSpec{
								UserSpec: rbac.UserSpec{
									Name: "e2e-namespace-regex",
									Kind: "User",
								},
							},
						},
					},
				},
			}

			EventuallyCreation(func() error {
				tnt.SetResourceVersion("")

				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenLabels: api.ForbiddenListSpec{
						Regex: annotationValue,
					},
				}
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			EventuallyDeletion(tnt)

			EventuallyCreation(func() error {
				tnt.SetResourceVersion("")

				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenAnnotations: api.ForbiddenListSpec{
						Regex: annotationValue,
					},
				}
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			EventuallyDeletion(tnt)
		})
	}
})
