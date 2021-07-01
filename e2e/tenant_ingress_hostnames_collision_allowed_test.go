//+build e2e

// Copyright 2020-2021 Clastix Labs
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1alpha1 "github.com/clastix/capsule/api/v1alpha1"

	capsulev1beta1 "github.com/clastix/capsule/api/v1beta1"
)

var _ = Describe("when a second Tenant contains an already declared allowed Ingress hostname", func() {
	tnt := &capsulev1beta1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "allowed-collision-ingress-hostnames",
		},
		Spec: capsulev1beta1.TenantSpec{
			Owner: capsulev1beta1.OwnerSpec{
				Name: "first-user",
				Kind: "User",
			},
			IngressHostnames: &capsulev1beta1.AllowedListSpec{
				Exact: []string{"capsule.clastix.io", "docs.capsule.k8s", "42.clatix.io"},
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

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.AllowTenantIngressHostnamesCollision = false
		})
	})

	It("should block creation if contains collided Ingress hostnames", func() {
		for i, h := range tnt.Spec.IngressHostnames.Exact {
			tnt2 := &capsulev1beta1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%d", tnt.GetName(), i),
				},
				Spec: capsulev1beta1.TenantSpec{
					Owner: capsulev1beta1.OwnerSpec{
						Name: "second-user",
						Kind: "User",
					},
					IngressHostnames: &capsulev1beta1.AllowedListSpec{
						Exact: []string{h},
					},
				},
			}
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt2)
			}).ShouldNot(Succeed())
		}
	})

	It("should not block creation if contains collided Ingress hostnames", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1alpha1.CapsuleConfiguration) {
			configuration.Spec.AllowTenantIngressHostnamesCollision = true
		})

		for i, h := range tnt.Spec.IngressHostnames.Exact {
			tnt2 := &capsulev1beta1.Tenant{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("%s-%d", tnt.GetName(), i),
				},
				Spec: capsulev1beta1.TenantSpec{
					Owner: capsulev1beta1.OwnerSpec{
						Name: "second-user",
						Kind: "User",
					},
					IngressHostnames: &capsulev1beta1.AllowedListSpec{
						Exact: []string{h},
					},
				},
			}
			EventuallyCreation(func() error {
				return k8sClient.Create(context.TODO(), tnt2)
			}).Should(Succeed())
			_ = k8sClient.Delete(context.TODO(), tnt2)
		}
	})
})
