// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

func priorityClassDefault(action rules.MutationAction, class string, conditions ...rules.AdmissionCondition) rules.NamespaceRuleMutation {
	return rules.NamespaceRuleMutation{Action: action, Conditions: conditions, Workloads: rules.WorkloadMutation{Classes: rules.WorkloadClassMutation{Priority: class}}}
}

func priorityClassProfile(profiles []string, body *rules.NamespaceRuleBodyNamespace) *rules.NamespaceRuleBodyTenant {
	return &rules.NamespaceRuleBodyTenant{
		NamespaceSelector:          &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "pc-profile", Operator: metav1.LabelSelectorOpIn, Values: profiles}}},
		NamespaceRuleBodyNamespace: body,
	}
}

// expectPriorityClass verifies the class together with the priority and
// preemption policy the API server resolved for it.
func expectPriorityClass(ctx context.Context, pod *corev1.Pod, class *schedulingv1.PriorityClass) {
	Eventually(func(g Gomega) {
		stored := &corev1.Pod{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
		g.Expect(stored.Spec.PriorityClassName).To(Equal(class.Name))
		g.Expect(stored.Spec.Priority).To(HaveValue(Equal(class.Value)))
		g.Expect(stored.Spec.PreemptionPolicy).To(Equal(class.PreemptionPolicy))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

var _ = Describe("PriorityClass default namespace profiles", Label("tenant", "rules", "workloads", "mutation", "priorityclass", "priority-class-defaults"), func() {
	It("defaults or replaces the class per namespace while retaining enforcement and tenant isolation", func() {
		ctx := context.Background()
		prefix := "e2e-pc-defaults-" + rand.String(8)

		newClass := func(name string, value int32, policy corev1.PreemptionPolicy) *schedulingv1.PriorityClass {
			class := &schedulingv1.PriorityClass{Name: name, Labels: map[string]string{"env": "e2e"}, Value: value, PreemptionPolicy: &policy, Description: "PriorityClass for e2e"}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(DeleteInBackground, class)

			return class
		}

		custom := newClass(prefix+"-custom", 500, corev1.PreemptLowerPriority)
		forbidden := newClass(prefix+"-forbidden", 100, corev1.PreemptLowerPriority)

		var (
			tenants  []*capsulev1beta2.Tenant
			owners   []client.Client
			defaults []*schedulingv1.PriorityClass
			forced   []*schedulingv1.PriorityClass
		)

		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			defaults = append(defaults, newClass(name+"-default", 1000, corev1.PreemptNever))
			forced = append(forced, newClass(name+"-forced", 2000, corev1.PreemptLowerPriority))

			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					priorityClassProfile([]string{"merge", "replace"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
						priorityClassDefault(rules.MutationActionMerge, "{{ .tenant.metadata.name }}-default"),
						priorityClassDefault(rules.MutationActionReplace, forbidden.Name, rules.AdmissionCondition{Name: "deny-class", Expression: `has(object.metadata.labels) && 'deny-class' in object.metadata.labels && object.metadata.labels['deny-class'] == 'true'`}),
					}}),
					priorityClassProfile([]string{"replace"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{priorityClassDefault(rules.MutationActionReplace, name+"-forced")}}),
					priorityClassProfile([]string{"merge", "replace"}, &rules.NamespaceRuleBodyNamespace{
						Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "not-the-priority-class-owner"}},
						Mutate:   []rules.NamespaceRuleMutation{priorityClassDefault(rules.MutationActionReplace, prefix+"-wrong-audience")},
					}),
					priorityClassProfile([]string{"merge", "replace"}, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
						Action:    rules.ActionTypeDeny,
						Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Priority: []rules.WorkloadClassMatch{{Exact: []string{forbidden.Name}}}}},
					}}),
					priorityClassProfile([]string{"missing"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{priorityClassDefault(rules.MutationActionMerge, prefix+"-absent")}}),
				},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}

		expectProfile := func(ns *corev1.Namespace, count int, class string) {
			waitPriorityClassProfile(ctx, ns, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
				g.Expect(bodies).To(HaveLen(count))

				if count > 0 {
					g.Expect(bodies[0].Mutate[0].Workloads.Classes.Priority).To(Equal(class))
				}
			})
		}
		newNamespace := func(tenant int, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[tenant].Name, "pc-profile": profile})
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)

			switch profile {
			case "merge":
				expectProfile(ns, 3, defaults[tenant].Name)
			case "replace":
				expectProfile(ns, 4, defaults[tenant].Name)
			case "missing":
				expectProfile(ns, 1, prefix+"-absent")
			default:
				expectProfile(ns, 0, "")
			}

			return ns
		}

		mergeNS, replaceNS := newNamespace(0, "merge"), newNamespace(0, "replace")
		otherNS, missingNS := newNamespace(0, "other"), newNamespace(0, "missing")
		tenantBNS := newNamespace(1, "merge")

		sequence := 0
		newPod := func(ns *corev1.Namespace, class string) *corev1.Pod {
			sequence++

			return priorityClassPod(ns.Name, fmt.Sprintf("pod-%d", sequence), class)
		}
		createPod := func(owner client.Client, ns *corev1.Namespace, class string, want *schedulingv1.PriorityClass) *corev1.Pod {
			pod := newPod(ns, class)
			Eventually(func() error { return owner.Create(ctx, pod) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectPriorityClass(ctx, pod, want)

			return pod
		}

		By("filling an omitted class and preserving an explicit one")
		original := createPod(owners[0], mergeNS, "", defaults[0])
		createPod(owners[0], mergeNS, custom.Name, custom)
		createPod(owners[0], mergeNS, defaults[0].Name, defaults[0])

		By("replacing every class in the replace profile")
		createPod(owners[0], replaceNS, "", forced[0])
		createPod(owners[0], replaceNS, custom.Name, forced[0])

		By("leaving non-selected namespaces and the other tenant's defaults alone")
		unselected := newPod(otherNS, "")
		Eventually(func() error { return owners[0].Create(ctx, unselected) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(unselected), stored)).To(Succeed())
			// A concurrent spec may own the cluster's global default; this profile has none.
			g.Expect(stored.Spec.PriorityClassName).NotTo(HavePrefix(prefix))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		createPod(owners[0], otherNS, custom.Name, custom)
		createPod(owners[1], tenantBNS, "", defaults[1])

		By("enforcing the final class for both supplied and mutated names")
		for _, mutated := range []bool{false, true} {
			pod := newPod(mergeNS, forbidden.Name)
			if mutated {
				pod.Spec.PriorityClassName = ""
				pod.Labels["deny-class"] = "true"
			}

			err := owners[0].Create(ctx, pod)
			Expect(err).To(MatchError(And(ContainSubstring(forbidden.Name), ContainSubstring("spec.priorityClassName"), ContainSubstring("is denied by namespace rule"))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		}

		By("rejecting a Pod whose default class does not exist")
		absent := newPod(missingNS, "")
		err := owners[0].Create(ctx, absent)
		Expect(err).To(MatchError(And(ContainSubstring("workloads.classes.priority"), ContainSubstring(fmt.Sprintf("PriorityClass %q does not exist", prefix+"-absent")))))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(absent), &corev1.Pod{}))).To(BeTrue())
		createPod(owners[0], missingNS, custom.Name, custom)

		By("rejecting cross-tenant Pod creation through RBAC")
		crossTenant := newPod(tenantBNS, "")
		err = owners[0].Create(ctx, crossTenant)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), &corev1.Pod{}))).To(BeTrue())

		By("rejecting an invalid class name without changing the active policy")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[0].Workloads.Classes.Priority = "Invalid/Class"

			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("an invalid priority class name was accepted")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("workloads.classes.priority"), ContainSubstring("invalid priority class name"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules[0].Mutate[0].Workloads.Classes.Priority).To(Equal("{{ .tenant.metadata.name }}-default"))

		By("applying a changed default only to newly created Pods")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}

			current.Spec.Rules[0].Mutate[0].Workloads.Classes.Priority = forced[0].Name

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(tenants[0], metav1.ConditionTrue, defaultTimeoutInterval)
		expectProfile(mergeNS, 3, forced[0].Name)
		createPod(owners[0], mergeNS, "", forced[0])
		Eventually(func() error {
			current := &corev1.Pod{}
			if err := owners[0].Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
				return err
			}

			current.Labels["updated"] = "true"

			return owners[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(original), stored)).To(Succeed())
			g.Expect(stored.Labels).To(HaveKeyWithValue("updated", "true"))
			g.Expect(stored.Spec.PriorityClassName).To(Equal(defaults[0].Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		createPod(owners[1], tenantBNS, "", defaults[1])

		By("selecting a different namespace profile after a label change")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(otherNS), current); err != nil {
				return err
			}

			current.Labels["pc-profile"] = "merge"

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectProfile(otherNS, 3, forced[0].Name)
		createPod(owners[0], otherNS, "", forced[0])
	})
})

// Only one PriorityClass can be the cluster's global default, and it is assigned
// to every Pod without a class, so this spec must not run next to others.
var _ = Describe("PriorityClass defaults with a cluster global default", Serial, Label("tenant", "rules", "workloads", "mutation", "priorityclass", "priority-class-defaults", "skip-on-openshift"), func() {
	It("treats the class Kubernetes assigned as omitted and still enforces it", func() {
		ctx := context.Background()
		prefix := "e2e-pc-global-" + rand.String(8)

		newClass := func(name string, value int32, global bool) *schedulingv1.PriorityClass {
			class := &schedulingv1.PriorityClass{Name: name, Labels: map[string]string{"env": "e2e"}, Value: value, GlobalDefault: global, PreemptionPolicy: new(corev1.PreemptLowerPriority), Description: "PriorityClass for e2e"}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(EventuallyDeletion, class)

			return class
		}

		tenantDefault := newClass(prefix+"-default", 1000, false)

		tnt := &capsulev1beta2.Tenant{Name: prefix, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix}},
			Rules: []*rules.NamespaceRuleBodyTenant{
				priorityClassProfile([]string{"merge"}, &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{priorityClassDefault(rules.MutationActionMerge, tenantDefault.Name)}}),
				priorityClassProfile([]string{"merge", "strict"}, &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Action:    rules.ActionTypeAllow,
					Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Classes: rules.WorkloadClassEnforcement{Priority: []rules.WorkloadClassMatch{{Exact: []string{tenantDefault.Name}}}}},
				}}),
			},
		}}
		Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
		DeferCleanup(DeleteInBackground, tnt)
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

		owner := impersonationClient(prefix, withDefaultGroups(nil))
		newNamespace := func(profile string, count int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "pc-profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			waitPriorityClassProfile(ctx, ns, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) { g.Expect(bodies).To(HaveLen(count)) })

			return ns
		}

		mergeNS, strictNS, otherNS := newNamespace("merge", 2), newNamespace("strict", 1), newNamespace("other", 0)

		By("admitting a Pod without a class under an allow list while no global default exists")
		existing := &schedulingv1.PriorityClassList{}
		Expect(k8sClient.List(ctx, existing)).To(Succeed())

		for _, class := range existing.Items {
			Expect(class.GlobalDefault).To(BeFalse(), "PriorityClass %s is already the cluster's global default", class.Name)
		}

		classless := priorityClassPod(strictNS.Name, "classless", "")
		Eventually(func() error { return owner.Create(ctx, classless) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(classless), stored)).To(Succeed())
			g.Expect(stored.Spec.PriorityClassName).To(BeEmpty())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		global := newClass(prefix+"-global", 10, true)

		By("replacing the class Kubernetes assigned, whether omitted or named explicitly")
		for i, class := range []string{"", global.Name} {
			pod := priorityClassPod(mergeNS.Name, fmt.Sprintf("defaulted-%d", i), class)
			Eventually(func() error { return owner.Create(ctx, pod) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectPriorityClass(ctx, pod, tenantDefault)
		}

		By("leaving the global default to namespaces without a class default")
		unselected := priorityClassPod(otherNS.Name, "unselected", "")
		Eventually(func() error { return owner.Create(ctx, unselected) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectPriorityClass(ctx, unselected, global)

		By("matching the assigned global default like an explicit class")
		denied := priorityClassPod(strictNS.Name, "denied", "")
		err := owner.Create(ctx, denied)
		Expect(err).To(MatchError(And(ContainSubstring(fmt.Sprintf("priority class %q at spec.priorityClassName is not allowed by namespace rule", global.Name)), ContainSubstring(tenantDefault.Name))))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(denied), &corev1.Pod{}))).To(BeTrue())

		allowed := priorityClassPod(strictNS.Name, "allowed", tenantDefault.Name)
		Eventually(func() error { return owner.Create(ctx, allowed) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectPriorityClass(ctx, allowed, tenantDefault)
	})
})
