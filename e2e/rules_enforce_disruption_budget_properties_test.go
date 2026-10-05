// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("PDB property namespace profiles", Label("tenant", "rules", "workloads", "disruption-budgets"), func() {
	It("checks percentage budgets, unhealthy policy, controller writes and scale without requiring a PDB", func() {
		ctx := context.Background()
		prefix := "e2e-pdb-properties-" + rand.String(8)
		body := &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets:           []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateStatefulSet},
			DisruptionBudgets: &rules.WorkloadDisruptionBudgetRules{AllowOverlap: new(false), EvictableReplicas: &rules.PlacementRange{Min: new(int64(1)), Max: new(int64(2))}, UnhealthyPodEvictionPolicies: []policyv1.UnhealthyPodEvictionPolicyType{policyv1.AlwaysAllow}},
			Placement:         rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{corev1.DefaultSchedulerName}}}},
		}}}
		a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "pdb-properties"}}, NamespaceRuleBodyNamespace: body}}}}
		b := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		By("rejecting an invalid eviction range at Tenant admission")
		invalidTenant := a.DeepCopy()
		invalidTenant.ObjectMeta = metav1.ObjectMeta{Name: prefix + "-invalid", Labels: map[string]string{"env": "e2e"}}
		invalidTenant.Status = capsulev1beta2.TenantStatus{}
		invalidTenant.Spec.Rules[0].Enforce.Workloads.DisruptionBudgets.EvictableReplicas.Min = new(int64(-1))
		Expect(k8sClient.Create(ctx, invalidTenant)).To(MatchError(ContainSubstring("evictableReplicas")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(invalidTenant), &capsulev1beta2.Tenant{}))).To(BeTrue())
		ownerA, ownerB := impersonationClient(a.Name, withDefaultGroups(nil)), impersonationClient(b.Name, withDefaultGroups(nil))
		waitProfile := func(ns *corev1.Namespace, selected bool) {
			Eventually(func(g Gomega) {
				rs := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, rs)).To(Succeed())
				g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
				if selected {
					g.Expect(rs.Status.Rules).To(HaveLen(1))
					g.Expect(rs.Status.Rules[0].Enforce.Workloads.DisruptionBudgets).To(Equal(body.Enforce.Workloads.DisruptionBudgets))
					g.Expect(rs.Status.Rules[0].Enforce.Workloads.Placement).To(Equal(body.Enforce.Workloads.Placement))
				} else {
					g.Expect(rs.Status.Rules).To(BeEmpty())
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, selected bool) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			waitProfile(ns, selected)
			return ns
		}
		selected, other, isolated := newNS(a, "pdb-properties", true), newNS(a, "other", false), newNS(b, "pdb-properties", false)
		deployment := func(ns *corev1.Namespace, name string, replicas int32) *appsv1.Deployment {
			labels := map[string]string{"app": name}
			return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: labels}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{NodeSelector: map[string]string{"capsule-e2e-unscheduled": prefix}, Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}}}}
		}
		budget := func(ns *corev1.Namespace, name string) *policyv1.PodDisruptionBudget {
			return &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}, MinAvailable: new(intstr.FromString("75%")), UnhealthyPodEvictionPolicy: new(policyv1.AlwaysAllow)}}
		}
		create := func(c client.Client, obj client.Object) {
			Expect(c.Create(ctx, obj)).To(Succeed())
			DeferCleanup(EventuallyDeletion, obj)
		}
		assertReplicas := func(c client.Client, obj *appsv1.Deployment, replicas int32) {
			Eventually(func(g Gomega) {
				current := &appsv1.Deployment{}
				g.Expect(c.Get(ctx, client.ObjectKeyFromObject(obj), current)).To(Succeed())
				g.Expect(current.Spec.Replicas).To(Equal(&replicas))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("allowing a controller without a PDB and checking a later budget")
		web := deployment(selected, "web", 4)
		create(ownerA, web)
		pdb := budget(selected, "web")
		create(ownerA, pdb)
		assertReplicas(ownerA, web, 4)
		By("composing PDB constraints with nested placement policies in the selected profile")
		for _, ns := range []*corev1.Namespace{selected, other, isolated} {
			actor := ownerA
			if ns == isolated {
				actor = ownerB
			}
			custom := deployment(ns, "custom-scheduler", 4)
			custom.Spec.Template.Spec.SchedulerName = "custom-scheduler"
			if ns == selected {
				Expect(actor.Create(ctx, custom)).To(MatchError(ContainSubstring("scheduler")))
				Expect(apierrors.IsNotFound(actor.Get(ctx, client.ObjectKeyFromObject(custom), &appsv1.Deployment{}))).To(BeTrue())
				continue
			}
			create(actor, custom)
			Eventually(func(g Gomega) {
				stored := &appsv1.Deployment{}
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(custom), stored)).To(Succeed())
				g.Expect(stored.Spec.Template.Spec.SchedulerName).To(Equal("custom-scheduler"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("checking the reverse order before controller Pods exist")
		create(ownerA, budget(selected, "future"))
		invalid := deployment(selected, "future", 3)
		Expect(ownerA.Create(ctx, invalid)).To(MatchError(ContainSubstring("PDB evictable replicas")))
		Expect(apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(invalid), &appsv1.Deployment{}))).To(BeTrue())
		By("rejecting a replica change on the main controller resource")
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(web), current)).To(Succeed())
			current.Spec.Replicas = new(int32(3))
			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("forbidden controller replica update succeeded")
			}
			g.Expect(err).To(MatchError(ContainSubstring("PDB evictable replicas")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		assertReplicas(ownerA, web, 4)
		By("checking scale writes used by kubectl and autoscalers, including both bounds and zero")
		for _, tc := range []struct {
			replicas int32
			denied   bool
		}{{3, true}, {12, true}, {0, false}, {4, false}} {
			Eventually(func(g Gomega) {
				current := &appsv1.Deployment{}
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(web), current)).To(Succeed())
				scale := &autoscalingv1.Scale{}
				g.Expect(ownerA.SubResource("scale").Get(ctx, current, scale)).To(Succeed())
				scale.Spec.Replicas = tc.replicas
				err := ownerA.SubResource("scale").Update(ctx, current, client.WithSubResourceBody(scale))
				if tc.denied {
					if err == nil {
						Fail("forbidden scale update succeeded")
					}
					g.Expect(err).To(MatchError(ContainSubstring("PDB evictable replicas")))
				} else {
					g.Expect(err).To(Succeed())
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			if tc.denied {
				assertReplicas(ownerA, web, 4)
			} else {
				assertReplicas(ownerA, web, tc.replicas)
			}
		}
		By("rejecting PDB budget and unhealthy-policy updates, preserving the stored spec")
		for _, field := range []string{"budget", "unhealthy"} {
			Eventually(func(g Gomega) {
				current := &policyv1.PodDisruptionBudget{}
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(pdb), current)).To(Succeed())
				want := "PDB evictable replicas"
				if field == "budget" {
					current.Spec.MinAvailable = new(intstr.FromString("100%"))
				} else {
					current.Spec.UnhealthyPodEvictionPolicy = nil
					want = "IfHealthyBudget"
				}
				err := ownerA.Update(ctx, current)
				if err == nil {
					Fail("forbidden PDB property update succeeded")
				}
				g.Expect(err).To(MatchError(ContainSubstring(want)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				current := &policyv1.PodDisruptionBudget{}
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(pdb), current)).To(Succeed())
				g.Expect(current.Spec.MinAvailable).To(Equal(new(intstr.FromString("75%"))))
				g.Expect(current.Spec.UnhealthyPodEvictionPolicy).To(Equal(new(policyv1.AlwaysAllow)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("rounding maxUnavailable percentages upward for a singleton")
		Eventually(func(g Gomega) {
			current := &policyv1.PodDisruptionBudget{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(pdb), current)).To(Succeed())
			current.Spec.MinAvailable = nil
			current.Spec.MaxUnavailable = new(intstr.FromString("25%"))
			g.Expect(ownerA.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(web), current)).To(Succeed())
			current.Spec.Replicas = new(int32(1))
			g.Expect(ownerA.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		assertReplicas(ownerA, web, 1)
		By("preserving different namespace profiles and tenant isolation")
		for _, fixture := range []struct {
			ns    *corev1.Namespace
			owner client.Client
		}{{other, ownerA}, {isolated, ownerB}} {
			create(fixture.owner, budget(fixture.ns, "web"))
			create(fixture.owner, deployment(fixture.ns, "web", 3))
		}
		By("applying a changed namespace profile to later controller writes")
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current)).To(Succeed())
			current.Labels["profile"] = "pdb-properties"
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, true)
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKey{Namespace: other.Name, Name: "web"}, current)).To(Succeed())
			current.Spec.Replicas = new(int32(2))
			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("new namespace profile was bypassed")
			}
			g.Expect(err).To(MatchError(ContainSubstring("PDB evictable replicas")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("allowing deletion to repair a blocking PDB and preserving owner RBAC")
		Expect(ownerA.Delete(ctx, pdb)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(pdb), &policyv1.PodDisruptionBudget{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(web), current)).To(Succeed())
			current.Spec.Replicas = new(int32(3))
			g.Expect(ownerA.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		assertReplicas(ownerA, web, 3)
		Expect(ownerA.Create(ctx, budget(selected, "web"))).To(MatchError(ContainSubstring("PDB evictable replicas")))
		Expect(apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: "web"}, &policyv1.PodDisruptionBudget{}))).To(BeTrue())
		Expect(ownerA.Create(ctx, budget(isolated, "cross-tenant"))).To(MatchError(ContainSubstring("cannot create resource \"poddisruptionbudgets\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: isolated.Name, Name: "cross-tenant"}, &policyv1.PodDisruptionBudget{}))).To(BeTrue())
	})
})
