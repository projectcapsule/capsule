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
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Administrators", Ordered, Label("namespace", "permissions", "administrators", "config"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt1 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tnt-admins-1",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-tnt-admins-1",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	tnt2 := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tnt-admins-2",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-tnt-admins-2",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	admin := rbac.UserSpec{
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

			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.Administrators = []rbac.UserSpec{admin}
		})

	})

	JustAfterEach(func() {
		for _, tnt := range []*capsulev1beta2.Tenant{tnt1, tnt2} {
			EventuallyDeletion(tnt)
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

	It("interaction with non-tenant namespaces", func() {
		ctx := context.TODO()

		ns1 := NewNamespace("", map[string]string{})

		By("creating namespace with explicit empty labels", func() {
			NamespaceCreation(ns1, admin, defaultTimeoutInterval).Should(Succeed())
		})

		By("verifying no ownerReferences and no tenant label", func() {
			ExpectNamespaceNotAssignedToTenant(ctx, ns1.Name)
		})

		By("updating unassigned namespace as administrator", func() {
			Eventually(func() error {
				current := NewNamespace(ns1.Name)

				if err := k8sClient.Get(ctx, types.NamespacedName{Name: ns1.Name}, current); err != nil {
					return err
				}

				original := current.DeepCopy()

				if current.Annotations == nil {
					current.Annotations = map[string]string{}
				}

				current.Annotations["e2e.capsule.clastix.io/admin-non-tenant-update"] = "true"

				return impersonationClient(admin.Name, nil).Patch(ctx, current, client.MergeFrom(original))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("verifying namespace is still not assigned after administrator update", func() {
			ExpectNamespaceNotAssignedToTenant(ctx, ns1.Name)
		})

		ns2 := NewNamespace("")

		By("creating namespace with nil labels", func() {
			NamespaceCreation(ns2, admin, defaultTimeoutInterval).Should(Succeed())
		})

		By("verifying no ownerReferences and no tenant label", func() {
			ExpectNamespaceNotAssignedToTenant(ctx, ns2.Name)
		})

		By("updating unassigned namespace with nil labels as administrator", func() {
			Eventually(func() error {
				current := NewNamespace(ns2.Name)

				if err := k8sClient.Get(ctx, types.NamespacedName{Name: ns2.Name}, current); err != nil {
					return err
				}

				original := current.DeepCopy()

				if current.Annotations == nil {
					current.Annotations = map[string]string{}
				}

				current.Annotations["e2e.capsule.clastix.io/admin-non-tenant-update"] = "true"

				return impersonationClient(admin.Name, nil).Patch(ctx, current, client.MergeFrom(original))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("verifying namespace is still not assigned after administrator update", func() {
			ExpectNamespaceNotAssignedToTenant(ctx, ns2.Name)
		})

		By("deleting namespace", func() {
			Expect(k8sClient.Delete(ctx, ns2)).Should(Succeed())
		})

		By("deleting namespace", func() {
			Expect(k8sClient.Delete(ctx, ns1)).Should(Succeed())
		})
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
			NamespaceIsPartOfTenant(tnt1, ns).Should(Succeed())
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
			NamespaceIsPartOfTenant(tnt1, ns1).Should(Succeed())

			Eventually(func(g Gomega) {
				t := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt1.GetName()}, t)).To(Succeed())
				g.Expect(t.Status.Size).To(Equal(uint(1)))

				instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{
					Name: ns1.GetName(),
					UID:  ns1.GetUID(),
				})
				g.Expect(instance).ToNot(BeNil(), "Namespace instance should not be nil")

				condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(condition).ToNot(BeNil(), "Condition instance should not be nil")

				g.Expect(instance.Name).To(Equal(ns1.GetName()))
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
				g.Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
				g.Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		ns2 := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt2.GetName(),
		})

		By("creating namespace", func() {
			NamespaceCreation(ns2, admin, defaultTimeoutInterval).Should(Succeed())
		})

		By("verifing tenant state", func() {
			NamespaceIsPartOfTenant(tnt2, ns2).Should(Succeed())

			Eventually(func(g Gomega) {
				t := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt2.GetName()}, t)).Should(Succeed())

				g.Expect(t.Status.Size).To(Equal(uint(1)))

				instance := t.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: ns2.GetName(), UID: ns2.GetUID()})
				g.Expect(instance).NotTo(BeNil(), "Namespace instance should not be nil")

				condition := instance.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(condition).NotTo(BeNil(), "Condition instance should not be nil")

				g.Expect(instance.Name).To(Equal(ns2.GetName()))
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue), "Expected namespace condition status to be True")
				g.Expect(condition.Type).To(Equal(meta.ReadyCondition), "Expected namespace condition type to be Ready")
				g.Expect(condition.Reason).To(Equal(meta.SucceededReason), "Expected namespace condition reason to be Succeeded")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		By("deleting namespace", func() {
			Expect(k8sClient.Delete(context.TODO(), ns2)).Should(Succeed())
		})

		By("deleting namespace", func() {
			Expect(k8sClient.Delete(context.TODO(), ns1)).Should(Succeed())
		})

	})
})
