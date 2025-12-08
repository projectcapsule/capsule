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
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("creating a Namespace as Tenant owner with custom --capsule-group", Label("config"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-assigned-custom-group",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "alice",
							Kind: "User",
						},
					},
				},
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "bob",
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
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())

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
			configuration.Spec.Users = []api.UserSpec{{Kind: api.GroupOwner, Name: "test"}}
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with multiple groups", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []api.UserSpec{{Kind: api.UserOwner, Name: "alice"}, {Kind: api.GroupOwner, Name: "test"}}
		})

		ns := NewNamespace("")

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	It("should succeed and be available in Tenant namespaces list with default single group", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []api.UserSpec{{Kind: api.GroupOwner, Name: "projectcapsule.dev"}}
		})

		ns := NewNamespace("")

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
	})

	It("should fail when group is ignored", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []api.UserSpec{{Kind: api.GroupOwner, Name: "projectcapsule.dev"}}
			configuration.Spec.IgnoreUserWithGroups = []string{"projectcapsule.dev"}
		})

		ns := NewNamespace("")

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with default single user", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []api.UserSpec{{Kind: api.UserOwner, Name: tnt.Spec.Owners[0].Name}}
			configuration.Spec.IgnoreUserWithGroups = []string{}
		})

		ns := NewNamespace("")

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
	})

	It("should succeed and be available in Tenant namespaces list with default single user", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.IgnoreUserWithGroups = []string{}
			configuration.Spec.Users = []api.UserSpec{{Kind: api.UserOwner, Name: tnt.Spec.Owners[0].Name}}
		})

		ns := NewNamespace("")

		NamespaceCreation(ns, tnt.Spec.Owners[1].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

	It("should fail when group is ignored", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.UserNames = []string{}
			configuration.Spec.UserGroups = []string{}
			configuration.Spec.Users = []api.UserSpec{{Kind: api.UserOwner, Name: tnt.Spec.Owners[0].Name}}
			configuration.Spec.IgnoreUserWithGroups = []string{"projectcapsule.dev"}
		})

		ns := NewNamespace("")

		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).ShouldNot(Succeed())
	})

})
