// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	otypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
)

var _ = Describe("Promoting ServiceAccounts", Label("config", "promotion"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-sa-promotion",
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				Promotions: capsulev1beta2.PromotionSpec{
					Rules: []*capsulev1beta2.PromotionRule{
						{
							ClusterRoles: []string{"view"},
						},
						{
							ClusterRoles: []string{"edit"},
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"selective": "edit",
								},
							},
						},
					},
				},
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "alice",
							Kind: "User",
						},
					},
				},
			},
			AdditionalRoleBindings: []rbac.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "admin",
					Subjects: []rbacv1.Subject{
						{
							Kind: "ServiceAccount",
							Name: "default",
						},
						{
							Kind: "User",
							Name: "bob",
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

	It("Verify Selective promotion (when feature is globally disabled)", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = false
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		time.Sleep(250 * time.Millisecond)

		// Create a ServiceAccount inside the tenant namespace
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		// Table of personas: client + expected result
		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"owner":   {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
			"rb-user": {client: impersonationClient("bob", withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
			"rb-sa":   {client: impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Trigger)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = meta.ValueTrue

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		t := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())

		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(t.Status.Promotions).To(HaveLen(0), "expected no promotions to be present")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(t.Status.Promotions).To(HaveLen(0), "expected no promotions to be present")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		for name, tc := range personas {
			By(fmt.Sprintf("trying to allow deletion SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(t.Status.Promotions).To(HaveLen(0), "expected no promotions to be present")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		for name, tc := range personas {
			By(fmt.Sprintf("trying to allow deletion SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "lala"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		Eventually(func(g Gomega) {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			g.Expect(t.Status.Promotions).To(HaveLen(0), "expected no promotions to be present")
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	})

	It("Verify Selective promotion (when feature is globally disabled)", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		time.Sleep(250 * time.Millisecond)

		secondary := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(secondary, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		time.Sleep(250 * time.Millisecond)

		// Create a ServiceAccount inside the tenant namespace
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		// Table of personas: client + expected result
		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"owner":   {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))), matcher: Succeed()},
			"rb-user": {client: impersonationClient("bob", withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
			"rb-sa":   {client: impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Trigger)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = meta.ValueTrue

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		t := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())

		Eventually(func(g Gomega) rbac.OwnerStatusListSpec {
			t := &capsulev1beta2.Tenant{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)
			g.Expect(err).NotTo(HaveOccurred())

			return t.Status.Promotions
		}, defaultTimeoutInterval, defaultPollInterval).Should(ContainElement(rbac.CoreOwnerSpec{
			UserSpec: rbac.UserSpec{
				Kind: rbac.ServiceAccountOwner,
				Name: "system:serviceaccount:" + sa.GetNamespace() + ":" + sa.GetName(),
			},
			ClusterRoles: []string{"view"},
		}), "expected ServiceAccount test-sa to be present in promotions")

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to allow deletion SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to allow deletion SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "lala"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}
	})

	It("Allow promotion by Owners", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		time.Sleep(250 * time.Millisecond)

		// Create a ServiceAccount inside the tenant namespace
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		// Table of personas: client + expected result
		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"rb-user": {client: impersonationClient("bob", withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
			"rb-sa":   {client: impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0))), matcher: Not(Succeed())},
			"owner":   {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))), matcher: Succeed()},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Trigger)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = meta.ValueTrue

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Generic)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}
	})

	It("Verify Rolebindings", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		time.Sleep(250 * time.Millisecond)

		// Create a ServiceAccount inside the tenant namespace
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		// Table of personas: client + expected result
		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"owner": {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))), matcher: Succeed()},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = meta.ValueTrue

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		time.Sleep(250 * time.Millisecond)

		VerifyTenantRoleBindings(tnt)
	})

	It("Verify Rolebindings (Custom Match)", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		time.Sleep(250 * time.Millisecond)

		// Create a ServiceAccount inside the tenant namespace
		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-sa",
				Namespace: ns.Name,
			},
		}
		Expect(k8sClient.Create(context.TODO(), sa)).Should(Succeed())

		// Table of personas: client + expected result
		personas := map[string]struct {
			client  client.Client
			matcher otypes.GomegaMatcher
		}{
			"owner": {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0))), matcher: Succeed()},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.ServiceAccountPromotionLabel] = meta.ValueTrue
				saCopy.Labels["selective"] = "edit"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		time.Sleep(250 * time.Millisecond)

		VerifyTenantRoleBindings(tnt)
	})

})
