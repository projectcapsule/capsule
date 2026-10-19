// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("namespace RBAC provisioning", Label("namespace-rbac-priority", "rolebindings", "namespace", "rules"), func() {
	It("provisions concurrent namespace profiles with owner access, selected permissions, resource policies and tenant isolation", func() {
		ctx := context.Background()
		admin, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		name := "e2e-rbac-priority-" + rand.String(6)
		readerName := name + "-reader"
		a := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}},
			Rules: []*rules.NamespaceRuleBodyTenant{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "reader"}},
				Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{
					ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: readerName}},
				}}},
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Action: rules.ActionTypeDeny, Services: rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}},
				}, Quota: []rules.ResourceQuotaRule{{Name: "service-count", Hard: corev1.ResourceList{corev1.ResourceServices: resource.MustParse("100")}}}},
			}},
			NetworkPolicies: api.NetworkPolicySpec{Items: []networkingv1.NetworkPolicySpec{{PodSelector: metav1.LabelSelector{}}}},
			LimitRanges: api.LimitRangesSpec{Items: []corev1.LimitRangeSpec{{Limits: []corev1.LimitRangeItem{{
				Type: corev1.LimitTypeContainer, DefaultRequest: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m")},
			}}}}},
			ResourceQuota: api.ResourceQuotaSpec{Scope: api.ResourceQuotaScopeNamespace, Items: []corev1.ResourceQuotaSpec{{
				Hard: corev1.ResourceList{corev1.ResourceConfigMaps: resource.MustParse("100")},
			}}},
		}}
		b := &capsulev1beta2.Tenant{Name: name + "-other", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name + "-other"}},
		}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		createNamespace := func(tnt *capsulev1beta2.Tenant, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "profile": profile})
			watchCtx, cancel := context.WithTimeout(ctx, defaultTimeoutInterval)
			defer cancel()
			bindings, err := admin.RbacV1().RoleBindings(ns.Name).Watch(watchCtx, metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer bindings.Stop()
			started := time.Now()
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			found := false
			for !found {
				select {
				case event, ok := <-bindings.ResultChan():
					Expect(ok).To(BeTrue(), "RoleBinding watch ended before owner access was provisioned")
					binding, ok := event.Object.(*rbacv1.RoleBinding)
					Expect(ok).To(BeTrue(), "unexpected RoleBinding watch event: %+v", event)
					for _, subject := range binding.Subjects {
						found = found || subject.Name == tnt.Spec.Owners[0].Name
					}
				case <-watchCtx.Done():
					Fail("owner RoleBinding was not provisioned: " + watchCtx.Err().Error())
				}
			}
			bindingLatency := time.Since(started)
			status := &capsulev1beta2.RuleStatus{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			owner := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(nil))
			if tnt.Name == a.Name && profile == "reader" {
				service := &corev1.Service{Name: "forbidden-first", Namespace: ns.Name, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 80}}}}
				Eventually(func(g Gomega) {
					err := owner.Create(ctx, service)
					if err == nil {
						Fail("owner access became available before namespace enforcement")
					}
					g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
					g.Expect(err).To(MatchError(ContainSubstring("denied by namespace rule")))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(service), &corev1.Service{}))).To(BeTrue())
			}
			cm := &corev1.ConfigMap{Name: "owner-access", Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}
			Eventually(func() error { return owner.Create(ctx, cm) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			fmt.Fprintf(GinkgoWriter, "Namespace %s profile=%s: owner RoleBinding observed after %s; owner write available within %s (poll=%s)\n", ns.Name, profile, bindingLatency, time.Since(started), defaultPollInterval)
			stored := &corev1.ConfigMap{}
			Expect(owner.Get(ctx, client.ObjectKeyFromObject(cm), stored)).To(Succeed())
			return ns
		}
		// Exercise concurrent namespace provisioning both within a Tenant and
		// across Tenants, checking enforcement on the first owner write above.
		profiles := []struct {
			tenant *capsulev1beta2.Tenant
			name   string
		}{{a, "reader"}, {a, "plain"}, {b, "reader"}}
		for range 2 {
			profiles = append(profiles, profiles...)
		}
		namespaces := make([]*corev1.Namespace, len(profiles))
		var provision sync.WaitGroup
		for i, profile := range profiles {
			provision.Go(func() {
				defer GinkgoRecover()
				namespaces[i] = createNamespace(profile.tenant, profile.name)
			})
		}
		provision.Wait()
		selected, plain, foreign := namespaces[0], namespaces[1], namespaces[2]
		TenantNamespaceReady(a, selected, 8)
		TenantNamespaceReady(b, foreign, 4)
		reader := impersonationClient(readerName, []string{"system:authenticated"})
		Eventually(func() error {
			return reader.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: "owner-access"}, &corev1.ConfigMap{})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		for _, ns := range []*corev1.Namespace{plain, foreign} {
			err := reader.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "owner-access"}, &corev1.ConfigMap{})
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
			Expect(err).To(MatchError(ContainSubstring(`cannot get resource "configmaps"`)))
		}
		ownerA := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		cross := &corev1.ConfigMap{Name: "cross-tenant", Namespace: foreign.Name}
		err = ownerA.Create(ctx, cross)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "configmaps"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.ConfigMap{}))).To(BeTrue())

		By("converging all resource phases and publishing effective enforcement")
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
			g.Expect(status.Status.Rules).To(HaveLen(1))
			policies := &networkingv1.NetworkPolicyList{}
			g.Expect(k8sClient.List(ctx, policies, client.InNamespace(selected.Name))).To(Succeed())
			g.Expect(policies.Items).To(HaveLen(1))
			g.Expect(policies.Items[0].Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
			foreignPolicies := &networkingv1.NetworkPolicyList{}
			g.Expect(k8sClient.List(ctx, foreignPolicies, client.InNamespace(foreign.Name))).To(Succeed())
			g.Expect(foreignPolicies.Items).To(BeEmpty(), "Tenant A policies must not appear in Tenant B")
			limits := &corev1.LimitRangeList{}
			g.Expect(k8sClient.List(ctx, limits, client.InNamespace(selected.Name))).To(Succeed())
			g.Expect(limits.Items).To(HaveLen(1))
			quotas := &corev1.ResourceQuotaList{}
			g.Expect(k8sClient.List(ctx, quotas, client.InNamespace(selected.Name))).To(Succeed())
			g.Expect(quotas.Items).To(HaveLen(2), "legacy and rule-generated quotas must both converge")
			g.Expect([]corev1.ResourceList{quotas.Items[0].Spec.Hard, quotas.Items[1].Spec.Hard}).To(ConsistOf(
				corev1.ResourceList{corev1.ResourceConfigMaps: resource.MustParse("100")},
				corev1.ResourceList{corev1.ResourceServices: resource.MustParse("100")},
			))
			generated := &capsulev1beta2.GlobalResourceQuota{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: a.Name + "-service-count"}, generated)).To(Succeed())
			g.Expect(generated.Spec.NamespaceSelectors[0].MatchLabels[meta.TenantLabel]).To(Equal(a.Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		service := &corev1.Service{Name: "forbidden", Namespace: selected.Name, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 80}}}}
		err = ownerA.Create(ctx, service)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
		Expect(err).To(MatchError(ContainSubstring("denied by namespace rule")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(service), &corev1.Service{}))).To(BeTrue())

		By("enforcing the profile while effective RuleStatus publication is pending")
		statusKey := client.ObjectKey{Namespace: selected.Name, Name: meta.NameForManagedRuleStatus()}
		published := &capsulev1beta2.RuleStatus{}
		Expect(k8sClient.Get(ctx, statusKey, published)).To(Succeed())
		restored := false
		restoreStatus := func() {
			if restored {
				return
			}
			Eventually(func() error {
				current := &capsulev1beta2.RuleStatus{}
				if err := k8sClient.Get(ctx, statusKey, current); err != nil {
					return err
				}
				current.Status.Rules = published.Status.Rules
				current.Status.ObservedGeneration = published.Status.ObservedGeneration
				return k8sClient.Status().Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			restored = true
		}
		DeferCleanup(restoreStatus)
		Eventually(func() error {
			current := &capsulev1beta2.RuleStatus{}
			if err := k8sClient.Get(ctx, statusKey, current); err != nil {
				return err
			}
			current.Status.Rules = nil
			current.Status.ObservedGeneration = 0
			return k8sClient.Status().Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		pendingService := service.DeepCopy()
		pendingService.Name = "forbidden-pending"
		err = ownerA.Create(ctx, pendingService)
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
		Expect(err).To(MatchError(ContainSubstring("denied by namespace rule")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pendingService), &corev1.Service{}))).To(BeTrue())
		pendingConfig := &corev1.ConfigMap{Name: "allowed-pending", Namespace: selected.Name, Labels: map[string]string{"env": "e2e"}}
		Expect(ownerA.Create(ctx, pendingConfig)).To(Succeed())
		Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(pendingConfig), &corev1.ConfigMap{})).To(Succeed())
		restoreStatus()

		// Capture stable common resources before exercising another full revision.
		stableBindings := &rbacv1.RoleBindingList{}
		Expect(k8sClient.List(ctx, stableBindings, client.InNamespace(selected.Name))).To(Succeed())
		var common *rbacv1.RoleBinding
		for i := range stableBindings.Items {
			if stableBindings.Items[i].RoleRef.Name == "admin" {
				common = &stableBindings.Items[i]
				break
			}
		}
		Expect(common).NotTo(BeNil())
		stablePolicies := &networkingv1.NetworkPolicyList{}
		Expect(k8sClient.List(ctx, stablePolicies, client.InNamespace(selected.Name))).To(Succeed())
		Expect(stablePolicies.Items).To(HaveLen(1))
		beforePolicy := stablePolicies.Items[0].DeepCopy()
		By("composing a non-selected profile without rewriting unchanged RBAC or defaulted policies")
		nonSelectedUser := a.Name + "-non-selected"
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			if len(current.Spec.Rules) == 1 {
				current.Spec.Rules = append(current.Spec.Rules, &rules.NamespaceRuleBodyTenant{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "absent"}},
					Permissions:       rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: "view", Subjects: []rbacv1.Subject{{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: nonSelectedUser}}}}},
				})
			}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			ready := current.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedBinding := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(common), storedBinding)).To(Succeed())
		Expect(storedBinding.ResourceVersion).To(Equal(common.ResourceVersion), "unchanged common binding was rewritten")
		storedPolicy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(beforePolicy), storedPolicy)).To(Succeed())
		Expect(storedPolicy.ResourceVersion).To(Equal(beforePolicy.ResourceVersion), "defaulted policy was rewritten")
		excluded := impersonationClient(nonSelectedUser, []string{"system:authenticated"})
		denied := excluded.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: "owner-access"}, &corev1.ConfigMap{})
		Expect(apierrors.IsForbidden(denied)).To(BeTrue(), "%v", denied)
		Expect(denied).To(MatchError(ContainSubstring(`cannot get resource "configmaps"`)))

		By("adding and removing permission profiles when namespace labels change")
		for _, profile := range []string{"reader", "plain"} {
			Eventually(func() error {
				current := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(plain), current); err != nil {
					return err
				}
				current.Labels["profile"] = profile
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				err := reader.Get(ctx, client.ObjectKey{Namespace: plain.Name, Name: "owner-access"}, &corev1.ConfigMap{})
				if profile == "reader" {
					g.Expect(err).NotTo(HaveOccurred())
				} else {
					g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
					g.Expect(err).To(MatchError(ContainSubstring(`cannot get resource "configmaps"`)))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
