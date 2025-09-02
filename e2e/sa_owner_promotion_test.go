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
	ctrlrbac "github.com/projectcapsule/capsule/controllers/rbac"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/meta"
)

var _ = Describe("Promoting ServiceAccounts to Owners", Label("config"), Label("promotion"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-owner-promotion",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: capsulev1beta2.OwnerListSpec{
				{
					Name: "alice",
					Kind: "User",
				},
			},
			AdditionalRoleBindings: []api.AdditionalRoleBindingsSpec{
				{
					ClusterRoleName: "cluster-admin",
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

	It("Deny Owner promotion even when feature is disabled", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = false
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
			"owner":   {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Not(Succeed())},
			"rb-user": {client: impersonationClient("bob", withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Not(Succeed())},
			"rb-sa":   {client: impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Not(Succeed())},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Trigger)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.OwnerPromotionLabelTrigger

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Any Value)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.OwnerPromotionLabel] = "false"

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
				saCopy.Labels[meta.OwnerPromotionLabel] = "false"

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
				saCopy.Labels[meta.OwnerPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}
	})

	It("Allow Owner promotion by Owners", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
			"rb-user": {client: impersonationClient("bob", withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Not(Succeed())},
			"rb-sa":   {client: impersonationClient("system:serviceaccount:"+sa.GetNamespace()+":default", withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Not(Succeed())},
			"owner":   {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Succeed()},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s (Setting Trigger)", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.OwnerPromotionLabelTrigger

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
				saCopy.Labels[meta.OwnerPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}
	})

	It("Allow Promoted ServiceAccount to interact with Tenant Namespaces", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("")
		NamespaceCreation(ns, tnt.Spec.Owners[0], defaultTimeoutInterval).Should(Succeed())
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
			"owner": {client: impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(make([]string, 0)), k8sClient.Scheme()), matcher: Succeed()},
		}

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.OwnerPromotionLabelTrigger

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		time.Sleep(250 * time.Millisecond)

		Eventually(func(g Gomega) []rbacv1.Subject {
			crb := &rbacv1.ClusterRoleBinding{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ctrlrbac.ProvisionerRoleName}, crb)
			g.Expect(err).NotTo(HaveOccurred())

			return crb.Subjects
		}, defaultTimeoutInterval, defaultPollInterval).Should(ContainElement(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "test-sa",
			Namespace: ns.Name,
		}), "expected ServiceAccount test-sa to be present in CRB subjects")

		saClient := impersonationClient(
			fmt.Sprintf("system:serviceaccount:%s:%s", ns.Name, sa.Name),
			nil,
			k8sClient.Scheme(),
		)

		newNs := NewNamespace("")
		Expect(saClient.Create(context.TODO(), newNs)).To(Succeed())
		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElements(ns.GetName()))

		Expect(saClient.Delete(context.TODO(), newNs)).To(Not(Succeed()))

		for name, tc := range personas {
			By(fmt.Sprintf("trying to promote SA as %s", name))

			Eventually(func() error {
				saCopy := &corev1.ServiceAccount{}
				Expect(tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)).To(Succeed())

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.OwnerPromotionLabel] = "false"

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)

			Eventually(func() (string, error) {
				latest := &corev1.ServiceAccount{}
				if err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(sa), latest); err != nil {
					return "", err
				}
				return latest.Labels[meta.OwnerPromotionLabel], nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Equal("false"), "expected label to be set for persona=%s", name)

		}

		Eventually(func(g Gomega) []rbacv1.Subject {
			crb := &rbacv1.ClusterRoleBinding{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: ctrlrbac.ProvisionerRoleName}, crb)
			g.Expect(err).NotTo(HaveOccurred())

			return crb.Subjects
		}, defaultTimeoutInterval, defaultPollInterval).Should(Not(ContainElement(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "test-sa",
			Namespace: ns.Name,
		})), "expected ServiceAccount test-sa not to be present in CRB subjects")

		time.Sleep(250 * time.Millisecond)

		secondNs := NewNamespace("")
		Eventually(func() error {
			return saClient.Create(context.TODO(), secondNs)
		}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())

		TenantNamespaceList(tnt, defaultTimeoutInterval).Should(Not(ContainElements(secondNs.GetName())))

		Expect(saClient.Delete(context.TODO(), secondNs)).To(Not(Succeed()))

	})

})
