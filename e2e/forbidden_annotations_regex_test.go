//go:build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("creating a tenant with various forbidden regexes", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: nil,
			Name:        "namespace",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owners: capsulev1beta1.OwnerListSpec{
				{
					Name: "alice",
					Kind: "User",
				},
			},
		},
	}

	It("should succeed when there are no annotations", func() {
		EventuallyCreation(func() error {
			tnt.ObjectMeta.Annotations = nil
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
	})

	annotationsToCheck := []string{
		capsulev1beta1.ForbiddenNamespaceAnnotationsRegexpAnnotation,
		capsulev1beta1.ForbiddenNamespaceLabelsRegexpAnnotation,
	}

	errorRegexes := []string{
		"(.*gitops|.*nsm).[k8s.io/((?!(resource)).*|trusted)](http://k8s.io/((?!(resource)).*%7Ctrusted))",
	}

	for _, annotation := range annotationsToCheck {
		for _, annotationValue := range errorRegexes {
			It("should fail using a non-valid the regex on the annotation "+annotation, func() {
				EventuallyCreation(func() error {
					tnt.ResourceVersion = ""
					tnt.ObjectMeta.Annotations = make(map[string]string)
					tnt.ObjectMeta.Annotations[annotation] = annotationValue
					return k8sClient.Create(context.TODO(), tnt)
				}).ShouldNot(Succeed())
			})
		}
	}

	successRegexes := []string{
		"",
		"(.*gitops|.*nsm)",
	}
	for _, annotation := range annotationsToCheck {
		for _, annotationValue := range successRegexes {
			It("should succeed using a valid regex on the annotation "+annotation, func() {
				EventuallyCreation(func() error {
					tnt.ResourceVersion = ""
					tnt.ObjectMeta.Annotations = make(map[string]string)
					tnt.ObjectMeta.Annotations[annotation] = annotationValue
					return k8sClient.Create(context.TODO(), tnt)
				}).Should(Succeed())
				Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
			})
		}
	}

})
