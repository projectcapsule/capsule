// Copyright 2020-2023 Project Capsule Authors.
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	otypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/users"
)

var _ = Describe("Promoting ServiceAccounts to Owners", Ordered, Label("config", "permissions", "owners", "promotion"), func() {
	originConfig := &capsulev1beta2.CapsuleConfiguration{}

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "e2e-tenant-owner-promotion",
			Labels: map[string]string{
				"env": "e2e",
			},
		},
		Spec: capsulev1beta2.TenantSpec{
			Permissions: capsulev1beta2.Permissions{
				AllowOwnerPromotion: true,
			},
			Owners: rbac.OwnerListSpec{
				{
					CoreOwnerSpec: rbac.CoreOwnerSpec{
						UserSpec: rbac.UserSpec{
							Name: "e2e-sa-owner-promotion",
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

	It("Deny Owner promotion even when feature is disabled", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = false
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

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
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.ValueTrue

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

	It("Deny Owner promotion even when feature is disabled on tenant", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		t := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(context.TODO(), types.NamespacedName{Name: tnt.GetName()}, t)).To(Succeed())
		t.Spec.Permissions.AllowOwnerPromotion = false
		Expect(k8sClient.Update(context.TODO(), t)).To(Succeed())

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

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
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.ValueTrue

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

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

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
				err := tc.client.Get(context.TODO(), client.ObjectKeyFromObject(sa), saCopy)
				if err != nil {
					return err
				}

				if saCopy.Labels == nil {
					saCopy.Labels = map[string]string{}
				}
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.ValueTrue

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

	DescribeTable("applies CapsuleUser metadata rules to tenant service accounts", Label("serviceaccount-audience"), func(promoted bool) {
		const policyKey = "openshift.io/run-level"
		const defaultKey = "example.corp/capsule-user"
		defaultValue := "true"
		ctx := context.Background()

		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "audience-sa", Namespace: ns.Name}}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())
		info := users.ServiceAccountUserInfo(ns.Name, sa.Name)
		// Use only Kubernetes service account groups. Adding the test suite's
		// Capsule group would hide a failure to recognize tenant service accounts.
		saClient := impersonationClient(info.Username, info.Groups)

		if promoted {
			By("promoting the service account through its tenant owner")
			owner := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(nil))
			patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]string{meta.OwnerPromotionLabel: meta.ValueTrue}}})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				return owner.Patch(ctx, sa, client.RawPatch(types.MergePatchType, patch))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
				g.Expect(current.Status.Owners.IsOwner(info.Username, info.Groups)).To(BeTrue())
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: originConfig.Spec.RBAC.ProvisionerClusterRole}, crb)).To(Succeed())
				g.Expect(crb.Subjects).To(ContainElement(rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: ns.Name}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		} else {
			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
			Expect(current.Status.Owners.IsOwner(info.Username, info.Groups)).To(BeFalse())
		}

		// Grant resource access independently of promotion so the unpromoted
		// account exercises admission, not an RBAC rejection.
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "audience-sa", Namespace: ns.Name},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "admin"},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: sa.Name, Namespace: ns.Name}},
		})).To(Succeed())

		audience := []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceCapsuleUser)}}
		UpdateTenantEventually(tnt, func(current *capsulev1beta2.Tenant) {
			current.Spec.Rules = []*rules.NamespaceRuleBodyTenant{
				{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Audience: audience,
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeDeny,
						Metadata: []rules.MetadataRule{{
							VersionKinds: runtime.VersionKinds{Kinds: []string{"*", "Namespace"}},
							Labels:       map[string]rules.MetadataValueRule{policyKey: {}},
							Annotations:  map[string]rules.MetadataValueRule{policyKey: {}},
						}},
					},
				}},
				{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Audience: audience,
					Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Metadata: []rules.MetadataRule{{
							VersionKinds: runtime.VersionKinds{Kinds: []string{"ConfigMap", "Namespace"}},
							Labels:       map[string]rules.MetadataValueRule{defaultKey: {Default: &defaultValue}},
						}},
					},
				}},
			}
		})
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, status)).To(Succeed())
			g.Expect(status.Status.Rules).To(HaveLen(2))
			g.Expect(status.Status.Rules[0].Audience).To(Equal(audience))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		kinds := []string{"ConfigMap"}
		if promoted {
			kinds = append(kinds, "Namespace")
		}
		for _, kind := range kinds {
			newResource := func() client.Object {
				if kind == "Namespace" {
					return NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name})
				}
				return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{GenerateName: "sa-audience-", Namespace: ns.Name}}
			}

			By(fmt.Sprintf("allowing %s creation without denied metadata and applying the CapsuleUser default", kind))
			existing := newResource()
			Eventually(func() error { return saClient.Create(ctx, existing) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(existing.GetLabels()).To(HaveKeyWithValue(defaultKey, defaultValue))

			By("allowing unrelated metadata edits and restoring the default on update")
			patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{defaultKey: nil, "example.corp/unrelated": ""}}})
			Expect(err).NotTo(HaveOccurred())
			Expect(saClient.Patch(ctx, existing, client.RawPatch(types.MergePatchType, patch))).To(Succeed())
			Expect(existing.GetLabels()).To(HaveKeyWithValue(defaultKey, defaultValue))

			for _, field := range []string{"labels", "annotations"} {
				for _, value := range []string{"privileged", ""} {
					expectDenied := func(err error) {
						Expect(err).To(HaveOccurred())
						Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected an admission denial: %v", err)
						Expect(err.Error()).To(ContainSubstring(fmt.Sprintf("metadata.%s[%q]", field, policyKey)))
						Expect(err.Error()).To(ContainSubstring("denied by namespace rule"))
					}
					By(fmt.Sprintf("denying %s creation with %s value %q", kind, field, value))
					candidate := newResource()
					if field == "labels" {
						labels := candidate.GetLabels()
						if labels == nil {
							labels = map[string]string{}
						}
						labels[policyKey] = value
						candidate.SetLabels(labels)
					} else {
						candidate.SetAnnotations(map[string]string{policyKey: value})
					}
					expectDenied(saClient.Create(ctx, candidate, client.DryRunAll))

					By(fmt.Sprintf("denying %s edits with %s value %q", kind, field, value))
					patch, err := json.Marshal(map[string]any{"metadata": map[string]any{field: map[string]string{policyKey: value}}})
					Expect(err).NotTo(HaveOccurred())
					expectDenied(saClient.Patch(ctx, existing, client.RawPatch(types.MergePatchType, patch), client.DryRunAll))
				}
			}
		}
	},
		Entry("promoted service account", true),
		Entry("unpromoted service account", false),
	)

	It("Allow Promoted ServiceAccount to interact with Tenant Namespaces", func() {
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			configuration.Spec.AllowServiceAccountPromotion = true
		})

		ns := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

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
				saCopy.Labels[meta.OwnerPromotionLabel] = meta.ValueTrue

				return tc.client.Update(context.TODO(), saCopy)
			}, defaultTimeoutInterval, defaultPollInterval).Should(tc.matcher, "persona=%s", name)
		}

		time.Sleep(250 * time.Millisecond)

		Eventually(func(g Gomega) []rbacv1.Subject {
			crb := &rbacv1.ClusterRoleBinding{}
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: originConfig.Spec.RBAC.ProvisionerClusterRole}, crb)
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
		)

		By("preventing the service account from deleting the namespace", func() {
			newNs := NewNamespace("", map[string]string{
				meta.TenantLabel: tnt.GetName(),
			})
			Expect(saClient.Create(context.TODO(), newNs)).To(Succeed())
			NamespaceIsPartOfTenant(tnt, newNs).Should(Succeed())

			Eventually(func(g Gomega) {
				// Deletion should eventually be forbidden / fail
				g.Expect(saClient.Delete(context.TODO(), newNs)).
					ToNot(Succeed())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

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
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: originConfig.Spec.RBAC.ProvisionerClusterRole}, crb)
			g.Expect(err).NotTo(HaveOccurred())

			return crb.Subjects
		}, defaultTimeoutInterval, defaultPollInterval).Should(Not(ContainElement(rbacv1.Subject{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      "test-sa",
			Namespace: ns.Name,
		})), "expected ServiceAccount test-sa not to be present in CRB subjects")

		secondNs := NewNamespace("", map[string]string{
			meta.TenantLabel: tnt.GetName(),
		})
		Eventually(func() error {
			return saClient.Create(context.TODO(), secondNs)
		}, defaultTimeoutInterval, defaultPollInterval).ShouldNot(Succeed())

		NamespaceIsNotPartOfTenant(tnt, secondNs).Should(Succeed())

		Expect(saClient.Delete(context.TODO(), secondNs)).To(Not(Succeed()))

	})
})
