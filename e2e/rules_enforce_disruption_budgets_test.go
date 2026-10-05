// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("PDB overlap namespace profiles", Label("tenant", "rules", "workloads", "disruption-budgets"), func() {
	It("checks PDBs, Pods and controller templates while preserving profiles and tenant boundaries", func() {
		ctx := context.Background()
		prefix := "e2e-pdb-" + rand.String(8)
		policy := func(allow bool) *rules.NamespaceRuleBodyTenant {
			return &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "pdb"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment, rules.ValidateStatefulSet}, DisruptionBudgets: &rules.WorkloadDisruptionBudgetRules{AllowOverlap: new(allow)}}}}}
		}
		a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{policy(false)}}}
		b := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		ownerA, ownerB := impersonationClient(a.Name, withDefaultGroups(nil)), impersonationClient(b.Name, withDefaultGroups(nil))
		waitProfile := func(ns *corev1.Namespace, count int) {
			Eventually(func(g Gomega) {
				rs := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, rs)).To(Succeed())
				g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
				g.Expect(rs.Status.Rules).To(HaveLen(count))
				if count > 0 {
					g.Expect(rs.Status.Rules[0].Enforce.Workloads.DisruptionBudgets.AllowOverlap).To(Equal(new(false)))
				}
				if count > 1 {
					g.Expect(rs.Status.Rules[1].Enforce.Workloads.DisruptionBudgets.AllowOverlap).To(Equal(new(true)))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, count int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			waitProfile(ns, count)
			return ns
		}
		selected, other, isolated := newNS(a, "pdb", 1), newNS(a, "other", 0), newNS(b, "pdb", 0)
		budget := func(ns *corev1.Namespace, name, app string) *policyv1.PodDisruptionBudget {
			return &policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: policyv1.PodDisruptionBudgetSpec{MaxUnavailable: new(intstr.FromInt32(1)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}}}}
		}
		pod := func(ns *corev1.Namespace, name, app string) *corev1.Pod {
			p := MakePod(ns.Name, name, map[string]string{"env": "e2e", "app": app}, nil, "registry.k8s.io/pause:3.10", "", "")
			p.Spec.NodeSelector = map[string]string{"pdb-e2e.example.com/node": prefix}
			return p
		}
		deployment := func(ns *corev1.Namespace, name, app string) *appsv1.Deployment {
			d := MakeDeployment(ns.Name, name, 0, map[string]string{"env": "e2e", "app": app}, "")
			d.Labels = map[string]string{"env": "e2e"}
			// The changing app label is not part of the immutable controller selector.
			d.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"instance": name}}
			d.Spec.Template.Labels["instance"] = name
			return d
		}
		create := func(actor client.Client, obj client.Object) {
			Expect(actor.Create(ctx, obj)).To(Succeed())
			Eventually(func(g Gomega) {
				stored := obj.DeepCopyObject().(client.Object)
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				g.Expect(stored.GetUID()).To(Equal(obj.GetUID()))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		deny := func(actor client.Client, obj client.Object) {
			Expect(actor.Create(ctx, obj)).To(MatchError(And(ContainSubstring("PDB overlap"), ContainSubstring("one"), ContainSubstring("two"))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj.DeepCopyObject().(client.Object)))).To(BeTrue())
		}
		By("allowing unoccupied selectors, then rejecting overlapping Pods and zero-replica templates")
		for _, fixture := range []struct {
			ns    *corev1.Namespace
			actor client.Client
		}{{selected, ownerA}, {other, ownerA}, {isolated, ownerB}} {
			create(fixture.actor, budget(fixture.ns, "one", "web"))
			create(fixture.actor, budget(fixture.ns, "two", "web"))
		}
		deny(ownerA, pod(selected, "blocked", "web"))
		deny(ownerA, deployment(selected, "blocked", "web"))
		d := deployment(selected, "stateful", "web")
		stateful := &appsv1.StatefulSet{ObjectMeta: d.ObjectMeta, Spec: appsv1.StatefulSetSpec{Replicas: new(int32(0)), ServiceName: "headless", Selector: d.Spec.Selector, Template: d.Spec.Template}}
		deny(ownerA, stateful)
		create(ownerA, pod(other, "allowed", "web"))
		create(ownerB, pod(isolated, "allowed", "web"))
		create(ownerB, deployment(isolated, "allowed", "web"))
		safe := pod(selected, "safe", "worker")
		create(ownerA, safe)
		safeDeployment := deployment(selected, "safe", "worker")
		create(ownerA, safeDeployment)
		By("rejecting Pod and template label changes and preserving stored labels")
		for _, obj := range []client.Object{safe, safeDeployment} {
			Eventually(func(g Gomega) {
				current := obj.DeepCopyObject().(client.Object)
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(obj), current)).To(Succeed())
				switch current := current.(type) {
				case *corev1.Pod:
					current.Labels["app"] = "web"
				case *appsv1.Deployment:
					current.Spec.Template.Labels["app"] = "web"
				}
				err := ownerA.Update(ctx, current)
				if err == nil {
					Fail("overlapping label update succeeded")
				}
				g.Expect(err).To(MatchError(ContainSubstring("PDB overlap")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				current := obj.DeepCopyObject().(client.Object)
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(obj), current)).To(Succeed())
				switch current := current.(type) {
				case *corev1.Pod:
					g.Expect(current.Labels["app"]).To(Equal("worker"))
				case *appsv1.Deployment:
					g.Expect(current.Spec.Template.Labels["app"]).To(Equal("worker"))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("checking labels changed through Pod status while allowing ordinary status updates")
		create(k8sClient, &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "pdb-status", Namespace: selected.Name}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods/status"}, Verbs: []string{"update", "patch"}}}})
		create(k8sClient, &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "pdb-status", Namespace: selected.Name}, RoleRef: rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: "pdb-status"}, Subjects: []rbacv1.Subject{{Kind: "User", Name: a.Name}}})
		Eventually(func(g Gomega) {
			current := &corev1.Pod{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(safe), current)).To(Succeed())
			g.Expect(ownerA.Status().Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &corev1.Pod{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(safe), current)).To(Succeed())
			current.Labels["app"] = "web"
			err := ownerA.Status().Update(ctx, current)
			if err == nil {
				Fail("overlapping Pod status label update succeeded")
			}
			g.Expect(err).To(MatchError(ContainSubstring("PDB overlap")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &corev1.Pod{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(safe), current)).To(Succeed())
			g.Expect(current.Labels["app"]).To(Equal("worker"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("checking PDB creation against existing zero-replica templates")
		create(ownerA, budget(selected, "worker-one", "worker"))
		workerTwo := budget(selected, "worker-two", "worker")
		Expect(ownerA.Create(ctx, workerTwo)).To(MatchError(ContainSubstring("PDB overlap")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(workerTwo), &policyv1.PodDisruptionBudget{}))).To(BeTrue())
		// Use an app represented only by a template to prove the reverse template lookup.
		create(ownerA, deployment(selected, "template-only", "template"))
		create(ownerA, budget(selected, "template-one", "template"))
		templateTwo := budget(selected, "template-two", "template")
		Expect(ownerA.Create(ctx, templateTwo)).To(MatchError(And(ContainSubstring("PDB overlap"), ContainSubstring("Deployment/template-only"))))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(templateTwo), &policyv1.PodDisruptionBudget{}))).To(BeTrue())
		By("rejecting a PDB selector update and allowing a selector repair")
		two := budget(selected, "two", "web")
		Eventually(func(g Gomega) {
			current := &policyv1.PodDisruptionBudget{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(two), current)).To(Succeed())
			current.Spec.Selector = &metav1.LabelSelector{} // selects worker as well
			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("overlapping selector update succeeded")
			}
			g.Expect(err).To(MatchError(ContainSubstring("PDB overlap")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &policyv1.PodDisruptionBudget{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(two), current)).To(Succeed())
			g.Expect(current.Spec.Selector.MatchLabels).To(Equal(map[string]string{"app": "web"}))
			current.Spec.Selector.MatchLabels["app"] = "separate"
			g.Expect(ownerA.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		create(ownerA, pod(selected, "repaired", "web"))
		By("applying changed namespace profiles to subsequent admissions")
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current)).To(Succeed())
			current.Labels["profile"] = "pdb"
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, 1)
		deny(ownerA, pod(other, "now-blocked", "web"))
		By("composing a later explicit overlap exception after a policy update")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			current.Spec.Rules = []*rules.NamespaceRuleBodyTenant{policy(false), policy(true)}
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, 2)
		create(ownerA, pod(other, "exception", "web"))
		By("restoring the policy and deleting a conflicting PDB without blocking remediation")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			current.Spec.Rules = []*rules.NamespaceRuleBodyTenant{policy(false)}
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, 1)
		Expect(ownerA.Delete(ctx, budget(other, "two", "web"))).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKey{Namespace: other.Name, Name: "two"}, &policyv1.PodDisruptionBudget{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		create(ownerA, pod(other, "after-delete", "web"))
		Expect(ownerA.Create(ctx, budget(other, "two", "web"))).To(MatchError(ContainSubstring("PDB overlap")))
		By("preserving cross-tenant authorization")
		cross := budget(isolated, "cross-tenant", "web")
		Expect(ownerA.Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource \"poddisruptionbudgets\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &policyv1.PodDisruptionBudget{}))).To(BeTrue())
	})
})
