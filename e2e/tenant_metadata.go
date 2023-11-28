//go:build e2e

// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
)

func getLabels(tnt capsulev1beta2.Tenant) (map[string]string, error) {
	current := &capsulev1beta2.Tenant{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, current)
	if err != nil {
		return nil, err
	}
	return current.GetLabels(), nil
}

var _ = Describe("adding metadata to a Tenant", func() {
	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-metadata",
			Labels: map[string]string{
				"custom-label": "test",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "jim",
					Kind: "User",
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

	It("Should ensure label metadata", func() {
		By("Default labels", func() {
			currentlabels, _ := getLabels(*tnt)
			Expect(currentlabels["kubernetes.io/metadata.name"]).To(Equal("tenant-metadata"))
			Expect(currentlabels["custom-label"]).To(Equal("test"))
		})
		By("Disallow name overwritte", func() {
			tnt.Labels["kubernetes.io/metadata.name"] = "evil"
			Expect(k8sClient.Update(context.TODO(), tnt)).ShouldNot(Succeed())
		})

	})
})
