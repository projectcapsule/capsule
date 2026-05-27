// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("creating a Namespace as Tenant owner with custom --capsule-group", Ordered, Label("config"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-assigned-custom-group",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-assigned-custom-group-1",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-assigned-custom-group-2",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
	})
	JustAfterEach(func() {
		EventuallyDeletion(tnt)

		// Restore Configuration
		Eventually(func() error {
			c := &capsulev1beta2.CapsuleConfiguration{}
			if err := k8sClient.Get(context.Background(), client.ObjectKey{Name: originConfig.Name}, c); err != nil {
				return err
			}
			// Apply the initial configuration from originConfig to c
			c.Spec = originConfig.Spec
			return k8sClient.Update(context.Background(), c)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("should fail using a User non matching the capsule-user-group flag", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.GroupOwner, Name: "test"}}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with multiple groups", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.UserOwner, Name: "e2e-assigned-custom-group-1"}, {Kind: rbac.GroupOwner, Name: "test"}}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with default single group", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"}}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
	})

	It("should fail when group is ignored", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.GroupOwner, Name: "projectcapsule.dev"}}
			configuration.Spec.IgnoreUserWithGroups = []string{"projectcapsule.dev"}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with default single user", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.UserOwner, Name: tnt.Spec.Owners[0].Name}}
			configuration.Spec.IgnoreUserWithGroups = []string{}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with default single user", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.IgnoreUserWithGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.UserOwner, Name: tnt.Spec.Owners[0].Name}}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[1].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
	})

	It("should fail when group is ignored", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []rbac.UserSpec{{Kind: rbac.UserOwner, Name: tnt.Spec.Owners[0].Name}}
			configuration.Spec.IgnoreUserWithGroups = []string{"projectcapsule.dev"}
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})

		NamespaceCreation(ns, tnt.Spec.Owners[1].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
		NamespaceIsNotPartOfTenant(tnt, ns).Should(Succeed())
	})

})
