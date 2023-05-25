//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/clastix/capsule/api/v1beta2"
	"github.com/clastix/capsule/pkg/api"
)

var _ = Describe("creating a tenant with various forbidden regexes", func() {
	//errorRegexes := []string{
	//	"(.*gitops|.*nsm).[k8s.io/((?!(resource)).*|trusted)](http://k8s.io/((?!(resource)).*%7Ctrusted))",
	//}
	//
	//for _, annotationValue := range errorRegexes {
	//	It("should fail using a non-valid the regex on the annotation", func() {
	//		tnt := &capsulev1beta2.Tenant{
	//			ObjectMeta: metav1.ObjectMeta{
	//				Name: "namespace",
	//			},
	//			Spec: capsulev1beta2.TenantSpec{
	//				Owners: capsulev1beta2.OwnerListSpec{
	//					{
	//						Name: "alice",
	//						Kind: "User",
	//					},
	//				},
	//			},
	//		}
	//
	//		EventuallyCreation(func() error {
	//			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
	//				ForbiddenLabels: api.ForbiddenListSpec{
	//					Regex: annotationValue,
	//				},
	//			}
	//			return k8sClient.Create(context.TODO(), tnt)
	//		}).ShouldNot(Succeed())
	//
	//		EventuallyCreation(func() error {
	//			tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
	//				ForbiddenAnnotations: api.ForbiddenListSpec{
	//					Regex: annotationValue,
	//				},
	//			}
	//			return k8sClient.Create(context.TODO(), tnt)
	//		}).ShouldNot(Succeed())
	//	})
	//}

	successRegexes := []string{
		"",
		"(.*gitops|.*nsm)",
	}
	for _, annotationValue := range successRegexes {
		It("should succeed using a valid regex on the annotation", func() {
			tnt := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: "namespace",
				},
				Spec: capsulev1beta2.TenantSpec{
					Owners: capsulev1beta2.OwnerListSpec{
						{
							Name: "alice",
							Kind: "User",
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
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())

			EventuallyCreation(func() error {
				tnt.SetResourceVersion("")

				tnt.Spec.NamespaceOptions = &capsulev1beta2.NamespaceOptions{
					ForbiddenAnnotations: api.ForbiddenListSpec{
						Regex: annotationValue,
					},
				}
				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		})
	}
})
