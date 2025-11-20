// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
)

var _ = Describe("Administrators", Label("namespace", "permissions"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-admins-1",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					UserSpec: api.UserSpec{
						Name: "paul",
						Kind: "User",
					},
				},
			},
		},
	}

	tnt2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tnt-admins-2",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					UserSpec: api.UserSpec{
						Name: "george",
						Kind: "User",
					},
				},
			},
		},
	}

	admin := api.UserSpec{
		Name: "admin",
		Kind: "User",
	}

	JustBeforeEach(func() {
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(context.TODO(), tnt)
			}).Should(Succeed())

		}

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.Administrators = []api.UserSpec{admin}
		})

	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		}

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

	It("capsule is triggered for administrators based on namespace label", func() {
		By("creating namespace with faulty label", func() {
			ns := NewNamespace("", map[string]string{
				meta.TenantLabel: "something-random",
			})

			NamespaceCreation(ns, admin, defaultTimeoutInterval).ShouldNot(Succeed())
		})

		By("creating namespace with no label", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, admin, defaultTimeoutInterval).Should(Succeed())

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
			Expect(len(ns.OwnerReferences)).To(Equal(0))
		})

		By("creating namespace with no label", func() {
			ns := NewNamespace("")
			NamespaceCreation(ns, admin, defaultTimeoutInterval).Should(Succeed())

			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: ns.GetName()}, ns)).Should(Succeed())
			Expect(len(ns.OwnerReferences)).To(Equal(0))

			PatchTenantLabelForNamespace(tnt1, ns, ownerClient(admin), defaultTimeoutInterval).Should(Succeed())

			NamespaceIsPartOfTenant(tnt1, ns)
		})
	})

	It("assignment from administrator should work for all tenants", func() {
		ns1 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt1.GetName(),
		})

		By("creating namespace", func() {
			NamespaceCreation(ns1, admin, defaultTimeoutInterval).Should(Succeed())
		})

		By("verifing tenant state", func() {
			TenantNamespaceList(tnt1, defaultTimeoutInterval).Should(ContainElements(ns1.GetName()))

			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).Should(Succeed())
			Expect(t.Status.Size).To(Equal(uint(1)))

			instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns1.GetName(), UID: ns1.GetUID()})
			Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

			condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

			Expect(instance.Name).To(Equal(ns1.GetName()))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
			Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
			Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
		})

		ns2 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt2.GetName(),
		})

		By("creating namespace", func() {
			NamespaceCreation(ns2, admin, defaultTimeoutInterval).Should(Succeed())
		})

		By("verifing tenant state", func() {
			TenantNamespaceList(tnt2, defaultTimeoutInterval).Should(ContainElements(ns2.GetName()))

			t := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

			Expect(t.Status.Size).To(Equal(uint(1)))

			instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns2.GetName(), UID: ns2.GetUID()})
			Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

			condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
			Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

			Expect(instance.Name).To(Equal(ns2.GetName()))
			Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
			Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
			Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
		})

		By("deleting namespace", func() {
			Expect(k8sClient.Delete(context.TODO(), ns2)).Should(Succeed())
		})

		By("deleting namespace", func() {
			Expect(k8sClient.Delete(context.TODO(), ns1)).Should(Succeed())
		})

	})
})
