// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("optional namespace rule booleans", Label("tenant", "rules", "enforce", "metadata", "optional-booleans"), func() {
	It("preserves omitted, false, and true behavior across namespace profiles and tenants", func() {
		ctx := context.Background()
		prefix := "e2e-rule-bools-" + rand.String(8)
		policyFromRules := func(bodies []any) map[string]any {
			body := bodies[0].(map[string]any)
			metadata := body["enforce"].(map[string]any)["metadata"].([]any)[0].(map[string]any)
			return metadata["labels"].(map[string]any)["team"].(map[string]any)
		}
		type fixture struct {
			tenant *capsulev1beta2.Tenant
			owner  client.Client
			ns     *corev1.Namespace
		}
		var fixtures []fixture
		for _, variant := range []string{"omitted", "false", "true"} {
			name := prefix + "-" + variant
			enabled := variant == "true"
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"boolean-profile": "selected"}},
					NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
						Action: rules.ActionTypeAllow,
						Metadata: []rules.MetadataRule{{
							APIGroups: []string{"v1"}, Kinds: []string{"ConfigMap"},
							Labels:      map[string]rules.MetadataValueRule{"team": {Required: enabled, Values: []apiruntime.ExpressionMatch{{Expression: "^alpha$", Negate: enabled}}}},
							Annotations: map[string]rules.MetadataValueRule{"example.corp/profile": {Default: new(name)}},
						}},
					}},
				}},
			}}
			wire, err := json.Marshal(tnt)
			Expect(err).NotTo(HaveOccurred())
			obj := &unstructured.Unstructured{}
			Expect(json.Unmarshal(wire, &obj.Object)).To(Succeed())
			obj.SetAPIVersion("capsule.clastix.io/v1beta2")
			obj.SetKind("Tenant")
			if variant == "false" {
				policy := policyFromRules(obj.Object["spec"].(map[string]any)["rules"].([]any))
				policy["required"] = false
				policy["values"].([]any)[0].(map[string]any)["negate"] = false
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, tnt)).To(Succeed())
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			storedTenant := obj.DeepCopy()
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name}, storedTenant)).To(Succeed())
			storedPolicy := policyFromRules(storedTenant.Object["spec"].(map[string]any)["rules"].([]any))
			storedMatch := storedPolicy["values"].([]any)[0].(map[string]any)
			if variant == "omitted" {
				Expect(storedPolicy).NotTo(HaveKey("required"))
				Expect(storedMatch).NotTo(HaveKey("negate"))
			} else if enabled {
				Expect(storedPolicy).To(HaveKeyWithValue("required", true))
				Expect(storedMatch).To(HaveKeyWithValue("negate", true))
			}
			owner := impersonationClient(name, withDefaultGroups(nil))
			var selected *corev1.Namespace
			for index, profile := range []string{"selected", "unselected"} {
				ns := NewNamespace("", map[string]string{meta.TenantLabel: name, "boolean-profile": profile})
				NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
				NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
				DeferCleanup(DeleteNamespaceInBackground, ns.Name)
				TenantNamespaceReady(tnt, ns, uint(index+1))
				Eventually(func() bool {
					return apierrors.IsNotFound(owner.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "boolean-rbac-readiness"}, &corev1.ConfigMap{}))
				}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
				Eventually(func(g Gomega) {
					rs := &capsulev1beta2.RuleStatus{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, rs)).To(Succeed())
					g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
					ready := rs.Status.Conditions.GetConditionByType(meta.ReadyCondition)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
					if profile == "selected" {
						g.Expect(rs.Status.Rules).To(HaveLen(1))
						policy := rs.Status.Rules[0].Enforce.Metadata[0].Labels["team"]
						g.Expect(policy.Required).To(Equal(enabled))
						g.Expect(policy.Values[0].Negate).To(Equal(enabled))
					} else {
						g.Expect(rs.Status.Rules).To(BeEmpty())
					}
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				if profile == "selected" {
					selected = ns
					raw := &unstructured.Unstructured{}
					raw.SetAPIVersion("capsule.clastix.io/v1beta2")
					raw.SetKind("RuleStatus")
					Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, raw)).To(Succeed())
					for _, bodies := range [][]any{raw.Object["spec"].([]any), raw.Object["status"].(map[string]any)["rules"].([]any)} {
						policy := policyFromRules(bodies)
						match := policy["values"].([]any)[0].(map[string]any)
						if enabled {
							Expect(policy).To(HaveKeyWithValue("required", true))
							Expect(match).To(HaveKeyWithValue("negate", true))
						} else {
							Expect(policy).NotTo(HaveKey("required"))
							Expect(match).NotTo(HaveKey("negate"))
						}
					}
				} else {
					unrestricted := &corev1.ConfigMap{Name: "unrestricted", Namespace: ns.Name, Labels: map[string]string{"team": "anything"}}
					Expect(owner.Create(ctx, unrestricted)).To(Succeed())
					persisted := &corev1.ConfigMap{}
					Expect(owner.Get(ctx, client.ObjectKeyFromObject(unrestricted), persisted)).To(Succeed())
					Expect(persisted.Annotations).NotTo(HaveKey("example.corp/profile"))
				}
			}
			allowed, denied := "alpha", "beta"
			if enabled {
				allowed, denied = denied, allowed
			}
			valid := &corev1.ConfigMap{Name: "valid", Namespace: selected.Name, Labels: map[string]string{"team": allowed}}
			Expect(owner.Create(ctx, valid)).To(Succeed())
			Eventually(func(g Gomega) {
				persisted := &corev1.ConfigMap{}
				g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(valid), persisted)).To(Succeed())
				g.Expect(persisted.Labels).To(HaveKeyWithValue("team", allowed))
				g.Expect(persisted.Annotations).To(HaveKeyWithValue("example.corp/profile", name))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			for _, value := range []string{"", denied} {
				candidate := &corev1.ConfigMap{Name: "missing", Namespace: selected.Name}
				if value != "" {
					candidate.Name = "denied"
					candidate.Labels = map[string]string{"team": value}
				}
				err := owner.Create(ctx, candidate)
				if value == "" && !enabled {
					Expect(err).NotTo(HaveOccurred())
				} else {
					Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
					Expect(err).To(MatchError(ContainSubstring("metadata label")))
					Expect(err).To(MatchError(ContainSubstring("team")))
					Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(candidate), &corev1.ConfigMap{}))).To(BeTrue())
				}
			}
			Eventually(func(g Gomega) {
				current := &corev1.ConfigMap{}
				g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(valid), current)).To(Succeed())
				current.Labels["team"] = denied
				err := owner.Update(ctx, current)
				if err == nil {
					Fail("metadata update violating the boolean policy was accepted")
				}
				g.Expect(apierrors.IsForbidden(err)).To(BeTrue())
				g.Expect(err).To(MatchError(ContainSubstring("metadata label")))
				g.Expect(err).To(MatchError(ContainSubstring("team")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			persisted := &corev1.ConfigMap{}
			Expect(owner.Get(ctx, client.ObjectKeyFromObject(valid), persisted)).To(Succeed())
			Expect(persisted.Labels).To(HaveKeyWithValue("team", allowed))
			fixtures = append(fixtures, fixture{tenant: tnt, owner: owner, ns: selected})
		}

		By("preserving tenant boundaries with different boolean profiles")
		crossTenant := &corev1.ConfigMap{Name: "cross-tenant", Namespace: fixtures[2].ns.Name, Labels: map[string]string{"team": "alpha"}}
		err := fixtures[0].owner.Create(ctx, crossTenant)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "configmaps"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), &corev1.ConfigMap{}))).To(BeTrue())

		By("updating an omitted flag to true and then returning to an omitted false value")
		for _, enabled := range []bool{true, false} {
			Eventually(func() error {
				current := &capsulev1beta2.Tenant{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(fixtures[0].tenant), current); err != nil {
					return err
				}
				current.Spec.Rules[0].Enforce.Metadata[0].Labels["team"] = rules.MetadataValueRule{Required: enabled, Values: []apiruntime.ExpressionMatch{{Expression: "^alpha$", Negate: enabled}}}
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			TenantReady(fixtures[0].tenant, metav1.ConditionTrue, defaultTimeoutInterval)
			Eventually(func(g Gomega) {
				rs := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: fixtures[0].ns.Name, Name: meta.NameForManagedRuleStatus()}, rs)).To(Succeed())
				g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
				g.Expect(rs.Status.Rules).To(HaveLen(1))
				g.Expect(rs.Status.Rules[0].Enforce.Metadata[0].Labels["team"].Required).To(Equal(enabled))
				g.Expect(rs.Status.Rules[0].Enforce.Metadata[0].Labels["team"].Values[0].Negate).To(Equal(enabled))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			value := "alpha"
			if enabled {
				value = "beta"
			}
			candidate := &corev1.ConfigMap{Name: "policy-" + value, Namespace: fixtures[0].ns.Name, Labels: map[string]string{"team": value}}
			Expect(fixtures[0].owner.Create(ctx, candidate)).To(Succeed())
			persisted := &corev1.ConfigMap{}
			Expect(fixtures[0].owner.Get(ctx, client.ObjectKeyFromObject(candidate), persisted)).To(Succeed())
			Expect(persisted.Labels).To(HaveKeyWithValue("team", value))
		}
	})
})
