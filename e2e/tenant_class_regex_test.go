// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("validating class allowed regex on tenant admission", Label("tenant", "classes", "regex"), func() {
	It("should deny tenant creation with invalid priorityClasses regex and allow valid regex", func() {
		invalidTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-priorityclass-invalid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				PriorityClasses: &api.DefaultAllowedListSpec{
					Regex: "[",
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), invalidTenant)
		}).Should(MatchError(ContainSubstring("unable to compile priorityClasses allowedRegex")))

		Consistently(func() error {
			return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(invalidTenant), &capsulev1beta2.Tenant{})
		}).ShouldNot(Succeed())

		validTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-priorityclass-valid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				PriorityClasses: &api.DefaultAllowedListSpec{
					Regex: "^gold-.*$",
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), validTenant)
		}).Should(Succeed())
		EventuallyDeletion(validTenant)
	})

	It("should deny tenant creation with invalid runtimeClasses regex and allow valid regex", func() {
		invalidTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-runtimeclass-invalid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				RuntimeClasses: &api.DefaultAllowedListSpec{
					Regex: "[",
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), invalidTenant)
		}).Should(MatchError(ContainSubstring("unable to compile runtimeClasses allowedRegex")))

		Consistently(func() error {
			return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(invalidTenant), &capsulev1beta2.Tenant{})
		}).ShouldNot(Succeed())

		validTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-runtimeclass-valid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				RuntimeClasses: &api.DefaultAllowedListSpec{
					Regex: "^kata-.*$",
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), validTenant)
		}).Should(Succeed())
		EventuallyDeletion(validTenant)
	})

	It("should deny tenant creation with invalid gatewayOptions.allowedClasses regex and allow valid regex", func() {
		invalidTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-gatewayclass-invalid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				GatewayOptions: capsulev1beta2.GatewayOptions{
					AllowedClasses: &api.DefaultAllowedListSpec{
						Regex: "[",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), invalidTenant)
		}).Should(MatchError(ContainSubstring("unable to compile gatewayClasses allowedRegex")))

		Consistently(func() error {
			return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(invalidTenant), &capsulev1beta2.Tenant{})
		}).ShouldNot(Succeed())

		validTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-gatewayclass-valid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				GatewayOptions: capsulev1beta2.GatewayOptions{
					AllowedClasses: &api.DefaultAllowedListSpec{
						Regex: "^gw-.*$",
					},
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), validTenant)
		}).Should(Succeed())
		EventuallyDeletion(validTenant)
	})

	It("should deny tenant creation with invalid deviceClasses regex and allow valid regex", func() {
		invalidTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-deviceclass-invalid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				DeviceClasses: &api.SelectorAllowedListSpec{
					Regex: "[",
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), invalidTenant)
		}).Should(MatchError(ContainSubstring("unable to compile deviceClasses allowedRegex")))

		Consistently(func() error {
			return k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(invalidTenant), &capsulev1beta2.Tenant{})
		}).ShouldNot(Succeed())

		validTenant := &capsulev1beta2.Tenant{
			ObjectMeta: NewObjectMeta("e2e-tnt-deviceclass-valid"),
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{
					{
						Name: "alice",
						Kind: "User",
					},
				},
				DeviceClasses: &api.SelectorAllowedListSpec{
					Regex: "^gpu-.*$",
				},
			},
		}

		EventuallyCreation(func() error {
			return k8sClient.Create(context.TODO(), validTenant)
		}).Should(Succeed())
		EventuallyDeletion(validTenant)
	})
})
