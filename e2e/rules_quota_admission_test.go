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

		It("rejects a Tenant rule update that decreases a hard limit while changing scope", func() {
			current := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, current)).To(Succeed())

			updated := current.DeepCopy()
			updated.Spec.Rules[0].NamespaceSelector = nil
			updated.Spec.Rules[0].Quota[0].Hard[corev1.ResourceLimitsCPU] = resource.MustParse("0")

			err := k8sClient.Update(ctx, updated)
			Expect(err).To(MatchError(ContainSubstring(
				`rules[0].quota[0].hard["limits.cpu"] cannot be reduced from 8 to 0 while namespace selectors are changing`,
			)))

			persisted := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tenantName}, persisted)).To(Succeed())
			Expect(persisted.Spec.Rules[0].NamespaceSelector).NotTo(BeNil())
			Expect(persisted.Spec.Rules[0].NamespaceSelector.MatchLabels).To(HaveKeyWithValue(selectorKey, "application"))
			persistedLimit := persisted.Spec.Rules[0].Quota[0].Hard[corev1.ResourceLimitsCPU]
			Expect(persistedLimit.Cmp(resource.MustParse("8"))).To(Equal(0))
		})

		It("rejects the same unsafe scope and hard-limit change on the generated quota", func() {
			controllerClient := impersonationClient(ControllerServiceAccountFull, nil)
			current := &capsulev1beta2.GlobalResourceQuota{}
			Expect(controllerClient.Get(ctx, quotaKey, current)).To(Succeed())

			updated := current.DeepCopy()
			delete(updated.Spec.NamespaceSelectors[0].LabelSelector.MatchLabels, selectorKey)
			updated.Spec.Quota.Hard[corev1.ResourceLimitsCPU] = resource.MustParse("0")

			err := controllerClient.Update(ctx, updated)
			Expect(err).To(MatchError(ContainSubstring(
				`spec.quota.hard["limits.cpu"] cannot be reduced from 8 to 0 while namespace selectors are changing`,
			)))

			persisted := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, persisted)).To(Succeed())
			Expect(persisted.Spec.NamespaceSelectors[0].LabelSelector.MatchLabels).To(HaveKeyWithValue(selectorKey, "application"))
			persistedLimit := persisted.Spec.Quota.Hard[corev1.ResourceLimitsCPU]
			Expect(persistedLimit.Cmp(resource.MustParse("8"))).To(Equal(0))
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

			err = tamperClient.Delete(ctx, current)
			Expect(err).To(MatchError(ContainSubstring(
				"Labeling resources as controller managed can only be done by the controller or administrators",
			)))

			persisted := &capsulev1beta2.GlobalResourceQuota{}
			Expect(k8sClient.Get(ctx, quotaKey, persisted)).To(Succeed())
			Expect(persisted.Annotations).NotTo(HaveKey("e2e.projectcapsule.dev/tampered"))
		})

		It("uses the owner reference to restore controller drift and keeps status reconciliation working", func() {
			controllerClient := impersonationClient(ControllerServiceAccountFull, nil)
			current := &capsulev1beta2.GlobalResourceQuota{}
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
