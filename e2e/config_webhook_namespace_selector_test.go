// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("Webhook namespace selectors", Serial, Label("config", "webhook-namespace-selectors"), func() {
	It("preserves conjunctive scope, mutation and validation independence, and tenant boundaries", func() {
		ctx := context.Background()
		prefix := "e2e-webhook-selectors-" + rand.String(6)
		const labelPrefix = "selectors.e2e.projectcapsule.dev/"
		var tenants []*capsulev1beta2.Tenant
		var owners []client.Client
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{"profile-default": {Default: new(name)}}}}}}},
					{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Metadata: []rules.MetadataRule{{Kinds: []string{"ConfigMap"}, Labels: map[string]rules.MetadataValueRule{"blocked": {Values: []apiruntime.ExpressionMatch{{Exact: []string{"true"}}}}}}}}}},
				},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		newNS := func(tenant int, excluded string) *corev1.Namespace {
			labels := map[string]string{meta.TenantLabel: tenants[tenant].Name}
			ns := NewNamespace("", labels)
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			Eventually(func(g Gomega) {
				status := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				g.Expect(status.Status.Rules).To(HaveLen(2))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			if excluded != "" {
				Eventually(func() error {
					current := &corev1.Namespace{}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ns), current); err != nil {
						return err
					}
					current.Labels[labelPrefix+excluded] = "true"
					return k8sClient.Update(ctx, current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			return ns
		}
		selected, globalSkip, mutatingSkip, validatingSkip, hookSkip := newNS(0, ""), newNS(0, "global"), newNS(0, "mutating"), newNS(0, "validating"), newNS(0, "hook")
		otherTenant := newNS(1, "")
		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(cfg *capsulev1beta2.CapsuleConfiguration) {
				cfg.Spec.Admission.Mutating = original.Spec.Admission.Mutating.DeepCopy()
				cfg.Spec.Admission.Validating = original.Spec.Admission.Validating.DeepCopy()
			})
		})
		selectors := map[string]*metav1.LabelSelector{}
		for _, kind := range []string{"mutating", "validating"} {
			selectors[kind] = &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				{Key: labelPrefix + "global", Operator: metav1.LabelSelectorOpDoesNotExist},
				{Key: labelPrefix + kind, Operator: metav1.LabelSelectorOpDoesNotExist},
				{Key: meta.TenantLabel, Operator: metav1.LabelSelectorOpExists},
				{Key: labelPrefix + "hook", Operator: metav1.LabelSelectorOpDoesNotExist},
			}}
		}
		ModifyCapsuleConfigurationOpts(func(cfg *capsulev1beta2.CapsuleConfiguration) {
			found := 0
			for _, hook := range cfg.Spec.Admission.Mutating.Webhooks {
				if hook.Name == "generic.rules.mutating.projectcapsule.dev" {
					hook.NamespaceSelector = selectors["mutating"].DeepCopy()
					found++
				}
			}
			for _, hook := range cfg.Spec.Admission.Validating.Webhooks {
				if hook.Name == "generic.rules.validating.projectcapsule.dev" {
					hook.NamespaceSelector = selectors["validating"].DeepCopy()
					found++
				}
			}
			Expect(found).To(Equal(2))
		})
		Eventually(func(g Gomega) {
			mutating := &admissionv1.MutatingWebhookConfiguration{}
			validating := &admissionv1.ValidatingWebhookConfiguration{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(original.Spec.Admission.Mutating.Name)}, mutating)).To(Succeed())
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(original.Spec.Admission.Validating.Name)}, validating)).To(Succeed())
			found := 0
			for _, hook := range mutating.Webhooks {
				if hook.Name == "generic.rules.mutating.projectcapsule.dev" {
					g.Expect(hook.NamespaceSelector).To(Equal(selectors["mutating"]))
					found++
				}
			}
			for _, hook := range validating.Webhooks {
				if hook.Name == "generic.rules.validating.projectcapsule.dev" {
					g.Expect(hook.NamespaceSelector).To(Equal(selectors["validating"]))
					found++
				}
			}
			g.Expect(found).To(Equal(2))
			for _, ns := range []*corev1.Namespace{globalSkip, validatingSkip, hookSkip} {
				probe := &corev1.ConfigMap{Name: "selector-ready", Namespace: ns.Name, Labels: map[string]string{"env": "e2e", "blocked": "true"}}
				g.Expect(owners[0].Create(ctx, probe, client.DryRunAll)).To(Succeed())
				wantDefault := ""
				if ns == validatingSkip {
					wantDefault = tenants[0].Name
				}
				g.Expect(probe.Labels["profile-default"]).To(Equal(wantDefault))
			}
			probe := &corev1.ConfigMap{Name: "mutation-selector-ready", Namespace: mutatingSkip.Name, Labels: map[string]string{"env": "e2e"}}
			g.Expect(owners[0].Create(ctx, probe, client.DryRunAll)).To(Succeed())
			g.Expect(probe.Labels).NotTo(HaveKey("profile-default"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		sequence := 0
		create := func(owner client.Client, ns *corev1.Namespace, blocked bool, wantDefault string) {
			sequence++
			obj := &corev1.ConfigMap{Name: fmt.Sprintf("selected-%d", sequence), Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}
			if blocked {
				obj.Labels["blocked"] = "true"
			}
			Expect(owner.Create(ctx, obj)).To(Succeed())
			Eventually(func(g Gomega) {
				stored := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				g.Expect(stored.Labels["profile-default"]).To(Equal(wantDefault))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		deny := func(ns *corev1.Namespace) {
			sequence++
			obj := &corev1.ConfigMap{Name: fmt.Sprintf("denied-%d", sequence), Namespace: ns.Name, Labels: map[string]string{"env": "e2e", "blocked": "true"}}
			Expect(owners[0].Create(ctx, obj)).To(MatchError(ContainSubstring("metadata label")))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
		}
		By("keeping all constraints conjunctive while mutating and validating scopes remain independent")
		create(owners[0], selected, false, tenants[0].Name)
		deny(selected)
		create(owners[0], globalSkip, true, "")
		create(owners[0], hookSkip, true, "")
		create(owners[0], mutatingSkip, false, "")
		deny(mutatingSkip)
		create(owners[0], validatingSkip, true, tenants[0].Name)
		create(owners[1], otherTenant, false, tenants[1].Name)
		cross := &corev1.ConfigMap{Name: "cross-tenant", Namespace: otherTenant.Name}
		Expect(owners[0].Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), cross))).To(BeTrue())
		By("applying namespace-label changes without changing the tenant's rules")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(globalSkip), current); err != nil {
				return err
			}
			delete(current.Labels, labelPrefix+"global")
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			probe := &corev1.ConfigMap{Name: "selector-enabled", Namespace: globalSkip.Name, Labels: map[string]string{"env": "e2e"}}
			g.Expect(owners[0].Create(ctx, probe, client.DryRunAll)).To(Succeed())
			g.Expect(probe.Labels["profile-default"]).To(Equal(tenants[0].Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		create(owners[0], globalSkip, false, tenants[0].Name)
		deny(globalSkip)
	})
})
