// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	tenantutils "github.com/projectcapsule/capsule/pkg/tenant"
)

var _ = Describe("rule-generated GlobalResourceQuota admission", Ordered,
	Label("resourcequota", "rules", "admission", "managed", "skip-on-openshift"), func() {
		const (
			tenantName  = "e2e-rule-quota-admission"
			quotaName   = "compute"
			selectorKey = "e2e.projectcapsule.dev/quota-scope"
			rbacName    = "e2e-rule-quota-admission-tamper"
		)

		ctx := context.Background()
		owner := rbac.UserSpec{Name: tenantName, Kind: rbac.UserOwner}
		tnt := &capsulev1beta2.Tenant{
			ObjectMeta: metav1.ObjectMeta{
				Name:   tenantName,
				Labels: map[string]string{"env": "e2e"},
			},
			Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: owner}}},
				Rules: []*rules.NamespaceRuleBodyTenant{{
					NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
						Quota: []rules.ResourceQuotaRule{{
							Name: quotaName,
							ResourceQuotaSpec: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
								corev1.ResourceLimitsCPU: resource.MustParse("8"),
								corev1.ResourcePods:      resource.MustParse("100"),
							}},
						}},
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{selectorKey: "application"},
					},
				}},
			},
		}
		quotaKey := client.ObjectKey{Name: tenantutils.RuleGlobalResourceQuotaName(tnt, quotaName)}
		tamperRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: rbacName},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{capsulev1beta2.GroupVersion.Group},
				Resources: []string{"globalresourcequotas"},
				Verbs:     []string{"get", "create", "update", "patch", "delete"},
			}},
		}
		setTenantQuota := func(selector *metav1.LabelSelector, cpu string) error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, current); err != nil {
				return err
			}

			current.Spec.Rules[0].NamespaceSelector = selector
			current.Spec.Rules[0].Quota[0].Hard[corev1.ResourceLimitsCPU] = resource.MustParse(cpu)

			return k8sClient.Update(ctx, current)
		}
		expectGeneratedQuota := func(scopeValue, hard, used string, namespaces ...string) {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalResourceQuota{}
				g.Expect(k8sClient.Get(ctx, quotaKey, current)).To(Succeed())
				g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
				g.Expect(current.Status.Namespaces).To(ConsistOf(namespaces))

				selector := current.Spec.NamespaceSelectors[0].LabelSelector
				g.Expect(selector).NotTo(BeNil())
				g.Expect(selector.MatchLabels).To(HaveKeyWithValue(meta.TenantLabel, tenantName))
				if scopeValue == "" {
					g.Expect(selector.MatchLabels).NotTo(HaveKey(selectorKey))
				} else {
					g.Expect(selector.MatchLabels).To(HaveKeyWithValue(selectorKey, scopeValue))
				}

				hardCPU := current.Spec.Quota.Hard[corev1.ResourceLimitsCPU]
				g.Expect(hardCPU.Cmp(resource.MustParse(hard))).To(Equal(0))
				usedCPU := current.Status.Total.Used[corev1.ResourceLimitsCPU]
				g.Expect(usedCPU.Cmp(resource.MustParse(used))).To(Equal(0))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		tamperBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: rbacName},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     rbacName,
			},
			Subjects: []rbacv1.Subject{{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     owner.Name,
			}},
		}

		BeforeAll(func() {
			EventuallyCreation(func() error {
				tamperRole.ResourceVersion = ""

				return k8sClient.Create(ctx, tamperRole)
			}).Should(Succeed())
			EventuallyCreation(func() error {
				tamperBinding.ResourceVersion = ""

				return k8sClient.Create(ctx, tamperBinding)
			}).Should(Succeed())

			EventuallyCreation(func() error {
				tnt.ResourceVersion = ""

				return k8sClient.Create(ctx, tnt)
			}).Should(Succeed())
			TenantReadyTrue(tnt)

			currentTenant := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, currentTenant)).To(Succeed())
			tnt.UID = currentTenant.UID

			By("waiting for the managed quota and its status to reconcile", func() {
				Eventually(func(g Gomega) {
					quota := &capsulev1beta2.GlobalResourceQuota{}
					g.Expect(k8sClient.Get(ctx, quotaKey, quota)).To(Succeed())
					g.Expect(quota.Labels).To(HaveKeyWithValue(meta.NewManagedByCapsuleLabel, meta.ValueController))
					g.Expect(metav1.IsControlledBy(quota, currentTenant)).To(BeTrue())
					g.Expect(quota.Status.ObservedGeneration).To(Equal(quota.Generation))

					condition := quota.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			})
		})

		AfterAll(func() {
			controllerClient := impersonationClient(ControllerServiceAccountFull, nil)
			forged := &capsulev1beta2.GlobalResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: rbacName}}
			Expect(ignoreNotFound(controllerClient.Delete(ctx, forged))).To(Succeed())

			EventuallyDeletion(tnt)
			EventuallyDeletion(tamperBinding)
			EventuallyDeletion(tamperRole)
		})

		It("rejects a Tenant rule update that decreases or removes a hard limit while changing scope", func() {
			for _, test := range []struct {
				name    string
				mutate  func(corev1.ResourceList)
				message string
			}{
				{
					name: "decrease",
					mutate: func(hard corev1.ResourceList) {
						hard[corev1.ResourceLimitsCPU] = resource.MustParse("0")
					},
					message: `rules[0].quota[0].hard["limits.cpu"] cannot be reduced from 8 to 0 while namespace selectors are changing`,
				},
				{
					name: "removal",
					mutate: func(hard corev1.ResourceList) {
						delete(hard, corev1.ResourceLimitsCPU)
					},
					message: `rules[0].quota[0].hard["limits.cpu"] cannot be removed while namespace selectors are changing`,
				},
			} {
				By(test.name, func() {
					current := &capsulev1beta2.Tenant{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, current)).To(Succeed())

					updated := current.DeepCopy()
					updated.Spec.Rules[0].NamespaceSelector = nil
					test.mutate(updated.Spec.Rules[0].Quota[0].Hard)

					err := k8sClient.Update(ctx, updated)
					Expect(err).To(MatchError(ContainSubstring(test.message)))
				})
			}

			persisted := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, persisted)).To(Succeed())
			Expect(persisted.Spec.Rules[0].NamespaceSelector).NotTo(BeNil())
			Expect(persisted.Spec.Rules[0].NamespaceSelector.MatchLabels).To(HaveKeyWithValue(selectorKey, "application"))
			persistedLimit := persisted.Spec.Rules[0].Quota[0].Hard[corev1.ResourceLimitsCPU]
			Expect(persistedLimit.Cmp(resource.MustParse("8"))).To(Equal(0))
		})

		It("rejects the same unsafe scope and hard-limit changes on the generated quota", func() {
			controllerClient := impersonationClient(ControllerServiceAccountFull, nil)
			for _, test := range []struct {
				name    string
				mutate  func(corev1.ResourceList)
				message string
			}{
				{
					name: "decrease",
					mutate: func(hard corev1.ResourceList) {
						hard[corev1.ResourceLimitsCPU] = resource.MustParse("0")
					},
					message: `spec.quota.hard["limits.cpu"] cannot be reduced from 8 to 0 while namespace selectors are changing`,
				},
				{
					name: "removal",
					mutate: func(hard corev1.ResourceList) {
						delete(hard, corev1.ResourceLimitsCPU)
					},
					message: `spec.quota.hard["limits.cpu"] cannot be removed while namespace selectors are changing`,
				},
			} {
				By(test.name, func() {
					current := &capsulev1beta2.GlobalResourceQuota{}
					Expect(controllerClient.Get(ctx, quotaKey, current)).To(Succeed())

					updated := current.DeepCopy()
					delete(updated.Spec.NamespaceSelectors[0].LabelSelector.MatchLabels, selectorKey)
					test.mutate(updated.Spec.Quota.Hard)

					err := controllerClient.Update(ctx, updated)
					Expect(err).To(MatchError(ContainSubstring(test.message)))
				})
			}

			persisted := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, persisted)).To(Succeed())
			Expect(persisted.Spec.NamespaceSelectors[0].LabelSelector.MatchLabels).To(HaveKeyWithValue(selectorKey, "application"))
			persistedLimit := persisted.Spec.Quota.Hard[corev1.ResourceLimitsCPU]
			Expect(persistedLimit.Cmp(resource.MustParse("8"))).To(Equal(0))
		})

		It("allows equal or increased limits with a scope change and a later same-scope decrease", func() {
			Expect(setTenantQuota(nil, "8")).To(Succeed())
			expectGeneratedQuota("", "8", "0")

			applicationSelector := &metav1.LabelSelector{MatchLabels: map[string]string{selectorKey: "application"}}
			Expect(setTenantQuota(applicationSelector, "9")).To(Succeed())
			expectGeneratedQuota("application", "9", "0")

			Expect(setTenantQuota(applicationSelector, "8")).To(Succeed())
			expectGeneratedQuota("application", "8", "0")
		})

		It("allows a scope-first transition, observes newly selected usage, and then rejects an unsafe decrease", func() {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenantName})
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())

			cs := ownerClient(owner)
			pod := MakePod(ns.Name, "scope-first-usage", nil, nil, "registry.k8s.io/pause:3.10", "", "")
			pod.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("300m"),
			}
			EventuallyCreation(func() error {
				_, err := cs.CoreV1().Pods(ns.Name).Create(ctx, pod, metav1.CreateOptions{})

				return err
			}).Should(Succeed())

			Expect(setTenantQuota(nil, "8")).To(Succeed())
			expectGeneratedQuota("", "8", "300m", ns.Name)

			err := setTenantQuota(nil, "0")
			Expect(err).To(MatchError(ContainSubstring(
				`rules[0].quota[0].hard["limits.cpu"] cannot be reduced to 0 while 300m is allocated`,
			)))

			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, current)).To(Succeed())
			delete(current.Spec.Rules[0].Quota[0].Hard, corev1.ResourceLimitsCPU)
			err = k8sClient.Update(ctx, current)
			Expect(err).To(MatchError(ContainSubstring(
				`rules[0].quota[0].hard["limits.cpu"] cannot be removed while 300m is allocated`,
			)))

			applicationSelector := &metav1.LabelSelector{MatchLabels: map[string]string{selectorKey: "application"}}
			Expect(setTenantQuota(applicationSelector, "8")).To(Succeed())
			expectGeneratedQuota("application", "8", "0")
		})

		It("denies authorized non-admin creation, updates, and deletes of managed quotas", func() {
			tamperClient := impersonationClient(owner.Name, withDefaultGroups(nil))
			current := &capsulev1beta2.GlobalResourceQuota{}
			Eventually(func() error {
				return tamperClient.Get(ctx, quotaKey, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			forged := &capsulev1beta2.GlobalResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:   rbacName,
					Labels: map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController},
				},
				Spec: capsulev1beta2.GlobalResourceQuotaSpec{
					Quota: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourceLimitsCPU: resource.MustParse("1"),
					}},
				},
			}
			err := tamperClient.Create(ctx, forged)
			Expect(err).To(MatchError(ContainSubstring(
				"Labeling resources as controller managed can only be done by the controller or administrators",
			)))

			updated := current.DeepCopy()
			updated.Annotations = map[string]string{"e2e.projectcapsule.dev/tampered": "true"}
			err = tamperClient.Update(ctx, updated)
			Expect(err).To(MatchError(ContainSubstring(
				"Labeling resources as controller managed can only be done by the controller or administrators",
			)))

			labelRemoval := current.DeepCopy()
			delete(labelRemoval.Labels, meta.NewManagedByCapsuleLabel)
			err = tamperClient.Update(ctx, labelRemoval)
			Expect(err).To(MatchError(ContainSubstring(
				"Labeling resources as controller managed can only be done by the controller or administrators",
			)))

			err = tamperClient.Delete(ctx, current)
			Expect(err).To(MatchError(ContainSubstring(
				"Labeling resources as controller managed can only be done by the controller or administrators",
			)))

			persisted := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, persisted)).To(Succeed())
			Expect(persisted.Annotations).NotTo(HaveKey("e2e.projectcapsule.dev/tampered"))
			Expect(persisted.Labels).To(HaveKeyWithValue(meta.NewManagedByCapsuleLabel, meta.ValueController))
		})

		It("does not apply managed protection to an unlabeled GlobalResourceQuota", func() {
			tamperClient := impersonationClient(owner.Name, withDefaultGroups(nil))
			unmanaged := &capsulev1beta2.GlobalResourceQuota{
				ObjectMeta: metav1.ObjectMeta{Name: rbacName},
				Spec: capsulev1beta2.GlobalResourceQuotaSpec{
					Quota: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourcePods: resource.MustParse("10"),
					}},
				},
			}
			Expect(tamperClient.Create(ctx, unmanaged)).To(Succeed())

			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(tamperClient.Get(ctx, client.ObjectKey{Name: unmanaged.Name}, current)).To(Succeed())
			current.Annotations = map[string]string{"e2e.projectcapsule.dev/updated": "true"}
			Expect(tamperClient.Update(ctx, current)).To(Succeed())
			Expect(tamperClient.Delete(ctx, current)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: unmanaged.Name}, &capsulev1beta2.GlobalResourceQuota{})

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})

		It("uses the owner reference to restore controller drift and keeps status reconciliation working", func() {
			controllerClient := impersonationClient(ControllerServiceAccountFull, nil)
			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(controllerClient.Get(ctx, quotaKey, current)).To(Succeed())

			scopeDrift := current.DeepCopy()
			delete(scopeDrift.Spec.NamespaceSelectors[0].LabelSelector.MatchLabels, selectorKey)
			Expect(controllerClient.Update(ctx, scopeDrift)).To(Succeed())
			Eventually(func(g Gomega) {
				reconciled := &capsulev1beta2.GlobalResourceQuota{}
				g.Expect(k8sClient.Get(ctx, quotaKey, reconciled)).To(Succeed())
				g.Expect(reconciled.Spec.NamespaceSelectors[0].LabelSelector.MatchLabels).
					To(HaveKeyWithValue(selectorKey, "application"))
				g.Expect(reconciled.Status.ObservedGeneration).To(Equal(reconciled.Generation))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			Expect(controllerClient.Get(ctx, quotaKey, current)).To(Succeed())
			originalGeneration := current.Generation

			drifted := current.DeepCopy()
			drifted.Spec.Quota.Hard[corev1.ResourceLimitsCPU] = resource.MustParse("9")
			Expect(controllerClient.Update(ctx, drifted)).To(Succeed())

			Eventually(func(g Gomega) {
				reconciled := &capsulev1beta2.GlobalResourceQuota{}
				g.Expect(k8sClient.Get(ctx, quotaKey, reconciled)).To(Succeed())
				g.Expect(reconciled.Generation).To(BeNumerically(">", originalGeneration))
				reconciledLimit := reconciled.Spec.Quota.Hard[corev1.ResourceLimitsCPU]
				g.Expect(reconciledLimit.Cmp(resource.MustParse("8"))).To(Equal(0))
				g.Expect(reconciled.Status.ObservedGeneration).To(Equal(reconciled.Generation))

				condition := reconciled.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})

		It("deletes the managed quota through the controller before the Tenant finalizes", func() {
			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, current)).To(Succeed())
			Expect(current.Finalizers).To(ContainElement(meta.ControllerFinalizer))

			EventuallyDeletion(tnt)
			Eventually(func() bool {
				err := k8sClient.Get(ctx, quotaKey, &capsulev1beta2.GlobalResourceQuota{})

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})

var _ = Describe("managed GlobalResourceQuota administrator admission", Ordered,
	Label("config", "resourcequota", "admission", "managed", "skip-on-openshift"), func() {
		const (
			adminName = "e2e-managed-global-quota-admin"
			quotaName = "e2e-managed-global-quota-admin"
		)

		ctx := context.Background()
		administrator := rbac.UserSpec{Name: adminName, Kind: rbac.UserOwner}
		originConfig := &capsulev1beta2.CapsuleConfiguration{}
		adminRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: adminName},
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{capsulev1beta2.GroupVersion.Group},
				Resources: []string{"globalresourcequotas"},
				Verbs:     []string{"get", "create", "update", "patch", "delete"},
			}},
		}
		adminBinding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: adminName},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     adminName,
			},
			Subjects: []rbacv1.Subject{{
				APIGroup: rbacv1.GroupName,
				Kind:     rbacv1.UserKind,
				Name:     adminName,
			}},
		}

		BeforeAll(func() {
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, originConfig)).To(Succeed())

			EventuallyCreation(func() error {
				adminRole.ResourceVersion = ""

				return k8sClient.Create(ctx, adminRole)
			}).Should(Succeed())
			EventuallyCreation(func() error {
				adminBinding.ResourceVersion = ""

				return k8sClient.Create(ctx, adminBinding)
			}).Should(Succeed())

			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Administrators = append(configuration.Spec.Administrators, administrator)
			})
		})

		AfterAll(func() {
			controllerClient := impersonationClient(ControllerServiceAccountFull, nil)
			quota := &capsulev1beta2.GlobalResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: quotaName}}
			Expect(ignoreNotFound(controllerClient.Delete(ctx, quota))).To(Succeed())

			Eventually(func() error {
				current := &capsulev1beta2.CapsuleConfiguration{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: originConfig.Name}, current); err != nil {
					return err
				}

				current.Spec = originConfig.Spec

				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			EventuallyDeletion(adminBinding)
			EventuallyDeletion(adminRole)
		})

		It("allows a configured administrator to create, update, and delete a managed quota", func() {
			adminClient := impersonationClient(administrator.Name, nil)
			quota := &capsulev1beta2.GlobalResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:   quotaName,
					Labels: map[string]string{meta.NewManagedByCapsuleLabel: meta.ValueController},
				},
				Spec: capsulev1beta2.GlobalResourceQuotaSpec{
					Quota: corev1.ResourceQuotaSpec{Hard: corev1.ResourceList{
						corev1.ResourcePods: resource.MustParse("10"),
					}},
				},
			}
			EventuallyCreation(func() error {
				quota.ResourceVersion = ""

				return adminClient.Create(ctx, quota)
			}).Should(Succeed())

			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(adminClient.Get(ctx, client.ObjectKey{Name: quotaName}, current)).To(Succeed())
			current.Annotations = map[string]string{"e2e.projectcapsule.dev/admin-updated": "true"}
			Expect(adminClient.Update(ctx, current)).To(Succeed())
			Expect(adminClient.Delete(ctx, current)).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKey{Name: quotaName}, &capsulev1beta2.GlobalResourceQuota{})

				return apierrors.IsNotFound(err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		})
	})
