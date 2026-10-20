// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("namespace provisioning failures", Label("namespace-rbac-priority", "managed", "rolebindings", "namespace"), func() {
	It("converges independent resource policies during RBAC failures and recovers namespace access", func() {
		ctx := context.Background()
		name := "e2e-rbac-failure-" + rand.String(6)
		binding := rbac.AdditionalRoleBindingsSpec{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: name + "-reader"}}}
		a := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}},
			Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Action:   rules.ActionTypeAllow,
					Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{"profile-owner": {Managed: new(name)}}}},
				}},
			}},
			NetworkPolicies: api.NetworkPolicySpec{Items: []networkingv1.NetworkPolicySpec{{PodSelector: metav1.LabelSelector{}}}},
			LimitRanges:     api.LimitRangesSpec{Items: []corev1.LimitRangeSpec{{Limits: []corev1.LimitRangeItem{{Type: corev1.LimitTypeContainer, DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")}, Max: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}}}}}},
			ResourceQuota:   api.ResourceQuotaSpec{Scope: api.ResourceQuotaScopeNamespace, Items: []corev1.ResourceQuotaSpec{{Hard: corev1.ResourceList{corev1.ResourceConfigMaps: resource.MustParse("10")}}}},
		}}
		b := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name + "-other"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		healthy := NewNamespace("", map[string]string{meta.TenantLabel: a.Name})
		NamespaceCreation(healthy, a.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(a, healthy, 1)
		foreign := NewNamespace("", map[string]string{meta.TenantLabel: b.Name})
		NamespaceCreation(foreign, b.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(b, foreign, 1)

		By("preparing an immutable RoleRef conflict before adding a permission rule")
		broken := NewNamespace("", map[string]string{meta.TenantLabel: a.Name})
		NamespaceCreation(broken, a.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(a, broken, 2)
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(broken), broken)).To(Succeed())
		DeferCleanup(ForceDeleteNamespace, ctx, broken.Name)
		conflict := &rbacv1.RoleBinding{
			Name: meta.NameForManagedRoleBindings(utils.RoleBindingHashFunc(binding)), Namespace: broken.Name,
			RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "edit"},
		}
		Expect(k8sClient.Create(ctx, conflict)).To(Succeed())
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Spec.Rules[0].Permissions.Bindings = []rbac.AdditionalRoleBindingsSpec{binding}
			current.Spec.NetworkPolicies.Items[0].PodSelector.MatchLabels = map[string]string{"revision": "after-rbac-failure"}
			current.Spec.LimitRanges.Items[0].Limits[0].DefaultRequest[corev1.ResourceCPU] = resource.MustParse("20m")
			current.Spec.ResourceQuota.Items[0].Hard[corev1.ResourceConfigMaps] = resource.MustParse("20")
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			status := current.Status.GetInstance(&capsulev1beta2.TenantStatusNamespaceItem{Name: broken.Name})
			g.Expect(status).NotTo(BeNil())
			g.Expect(status.UID).To(Equal(broken.UID))
			ready := status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(ready.Reason).To(Equal(meta.PermissionsFailedReason))
			g.Expect(ready.Message).To(ContainSubstring("cannot change roleRef"))
			policies, limits, quotas := &networkingv1.NetworkPolicyList{}, &corev1.LimitRangeList{}, &corev1.ResourceQuotaList{}
			for _, list := range []client.ObjectList{policies, limits, quotas} {
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(broken.Name))).To(Succeed())
			}
			g.Expect(policies.Items).To(HaveLen(1))
			g.Expect(policies.Items[0].Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("revision", "after-rbac-failure"))
			g.Expect(limits.Items).To(HaveLen(1))
			g.Expect(limits.Items[0].Spec.Limits[0].DefaultRequest[corev1.ResourceCPU]).To(Equal(resource.MustParse("20m")))
			g.Expect(quotas.Items).To(HaveLen(1))
			g.Expect(quotas.Items[0].Spec.Hard[corev1.ResourceConfigMaps]).To(Equal(resource.MustParse("20")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		owner := impersonationClient(a.Name, withDefaultGroups(nil))
		cm := &corev1.ConfigMap{Name: "healthy-access", Namespace: healthy.Name}
		Expect(owner.Create(ctx, cm)).To(Succeed())
		Expect(owner.Get(ctx, client.ObjectKeyFromObject(cm), &corev1.ConfigMap{})).To(Succeed())
		reader := impersonationClient(binding.Subjects[0].Name, []string{"system:authenticated"})
		denied := reader.Get(ctx, client.ObjectKey{Namespace: broken.Name, Name: "no-access"}, &corev1.ConfigMap{})
		Expect(apierrors.IsForbidden(denied)).To(BeTrue(), "%v", denied)
		Expect(denied).To(MatchError(ContainSubstring(`cannot get resource "configmaps"`)))

		By("preserving LimitRange defaults and rejecting resource violations")
		pod := &corev1.Pod{Name: "defaulted", Namespace: healthy.Name, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10"}}}}
		Expect(owner.Create(ctx, pod)).To(Succeed())
		stored := &corev1.Pod{}
		Expect(owner.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
		Expect(stored.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("20m")))
		invalid := &corev1.Pod{Name: "too-large", Namespace: healthy.Name, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2")}}}}}}
		err := owner.Create(ctx, invalid)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
		Expect(err).To(MatchError(ContainSubstring("maximum cpu usage per Container")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(invalid), &corev1.Pod{}))).To(BeTrue())

		By("recovering RBAC without applying Tenant A's profile to Tenant B")
		Expect(k8sClient.Delete(ctx, conflict)).To(Succeed())
		TenantNamespaceReady(a, broken, 2)
		target := &corev1.ConfigMap{Name: "recovered", Namespace: broken.Name}
		Eventually(func() error { return owner.Create(ctx, target) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error { return reader.Get(ctx, client.ObjectKeyFromObject(target), &corev1.ConfigMap{}) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("mutating through the single-handler route while RuleStatus is pending")
		key := client.ObjectKey{Namespace: broken.Name, Name: meta.NameForManagedRuleStatus()}
		published := &capsulev1beta2.RuleStatus{}
		Expect(k8sClient.Get(ctx, key, published)).To(Succeed())
		DeferCleanup(func() {
			Eventually(func() error {
				current := &capsulev1beta2.RuleStatus{}
				if err := k8sClient.Get(ctx, key, current); err != nil {
					return err
				}
				current.Status = published.DeepCopy().Status
				return k8sClient.Status().Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		})
		Eventually(func() error {
			current := &capsulev1beta2.RuleStatus{}
			if err := k8sClient.Get(ctx, key, current); err != nil {
				return err
			}
			current.Status.Rules = nil
			current.Status.ObservedGeneration = 0
			return k8sClient.Status().Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		pending := &corev1.ConfigMap{Name: "pending-profile", Namespace: broken.Name}
		Expect(owner.Create(ctx, pending)).To(Succeed())
		storedConfig := &corev1.ConfigMap{}
		Expect(owner.Get(ctx, client.ObjectKeyFromObject(pending), storedConfig)).To(Succeed())
		Expect(storedConfig.Labels["profile-owner"]).To(Equal(a.Name))
		foreignConfig := &corev1.ConfigMap{Name: "foreign-profile", Namespace: foreign.Name}
		otherOwner := impersonationClient(b.Name, withDefaultGroups(nil))
		Expect(otherOwner.Create(ctx, foreignConfig)).To(Succeed())
		foreignStored := &corev1.ConfigMap{}
		Expect(otherOwner.Get(ctx, client.ObjectKeyFromObject(foreignConfig), foreignStored)).To(Succeed())
		Expect(foreignStored.Labels).NotTo(HaveKey("profile-owner"))
		denied = reader.Get(ctx, client.ObjectKey{Namespace: foreign.Name, Name: "foreign"}, &corev1.ConfigMap{})
		Expect(apierrors.IsForbidden(denied)).To(BeTrue(), "%v", denied)
		Expect(denied).To(MatchError(ContainSubstring(`cannot get resource "configmaps"`)))
		foreignPolicies := &networkingv1.NetworkPolicyList{}
		Expect(k8sClient.List(ctx, foreignPolicies, client.InNamespace(foreign.Name))).To(Succeed())
		Expect(foreignPolicies.Items).To(BeEmpty())
	})
})
