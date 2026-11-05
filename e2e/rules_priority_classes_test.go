// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// priorityClassPod never starts: the scheduling gate keeps it Pending, so the
// spec only exercises admission.
func priorityClassPod(namespace, name, class string) *corev1.Pod {
	return &corev1.Pod{
		Name: name, Namespace: namespace, Labels: map[string]string{"env": "e2e"},
		Spec: corev1.PodSpec{
			PriorityClassName: class,
			SchedulingGates:   []corev1.PodSchedulingGate{{Name: "example.com/e2e-priority-class"}},
			SecurityContext:   nobodyPodSecurityContext(),
			Containers:        []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
		},
	}
}

func waitPriorityClassProfile(ctx context.Context, ns *corev1.Namespace, check func(Gomega, []*rules.NamespaceRuleBodyNamespace)) {
	Eventually(func(g Gomega) {
		status := &capsulev1beta2.RuleStatus{}
		g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, status)).To(Succeed())
		g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))

		condition := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))

		check(g, status.Status.Rules)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}

var _ = Describe("PriorityClass namespace profiles", Label("tenant", "rules", "workloads", "priorityclass", "priority-class-rules"), func() {
	It("enforces composed profiles, changes, and tenant boundaries", func() {
		ctx := context.Background()
		prefix := "e2e-pc-rules-" + rand.String(8)
		gold, silver, bronze := prefix+"-gold", prefix+"-silver", prefix+"-bronze"
		premium := map[string]string{"pc-tier": prefix + "-premium"}

		for name, labels := range map[string]map[string]string{gold: premium, silver: {"pc-tier": prefix + "-standard"}, bronze: nil} {
			class := &schedulingv1.PriorityClass{Name: name, Labels: mergeMaps(map[string]string{"env": "e2e"}, labels), Value: 1000, Description: "PriorityClass for e2e"}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(DeleteInBackground, class)
		}

		policy := func(profile string, action rules.ActionType, targets []rules.WorkloadValidationTarget, matches ...rules.WorkloadClassMatch) *rules.NamespaceRuleBodyTenant {
			body := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action:    action,
				Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: targets, Classes: rules.WorkloadClassEnforcement{Priority: matches}},
			}}}
			if profile != "" {
				body.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"pc-profile": profile}}
			}

			return body
		}

		a := &capsulev1beta2.Tenant{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{
			policy("named", rules.ActionTypeDeny, nil, rules.WorkloadClassMatch{Expression: "^" + prefix + "-"}),
			policy("named", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{Exact: []string{gold}}),
			policy("named", rules.ActionTypeAudit, nil, rules.WorkloadClassMatch{Exact: []string{gold, bronze}}),
			policy("selected", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{MatchLabels: premium}),
			policy("templates", rules.ActionTypeAllow, []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment}, rules.WorkloadClassMatch{Exact: []string{silver}}),
			// An unrelated conditional rule that matches every Pod of this spec: it must
			// not make updates check a class they keep.
			{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pc-profile": "named"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action:     rules.ActionTypeDeny,
				Conditions: []rules.AdmissionCondition{{Name: "e2e-pods", Expression: `has(object.metadata.labels) && 'env' in object.metadata.labels && object.metadata.labels['env'] == 'e2e'`}},
				Workloads:  rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{prefix + "-blocked-scheduler"}}}}},
			}}},
		}}}
		b := &capsulev1beta2.Tenant{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}, Rules: []*rules.NamespaceRuleBodyTenant{
			policy("", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{Exact: []string{bronze}}),
		}}}

		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		ownerA := impersonationClient(a.Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Name, withDefaultGroups(nil))

		waitProfile := func(ns *corev1.Namespace, rulesWanted int) {
			waitPriorityClassProfile(ctx, ns, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
				g.Expect(bodies).To(HaveLen(rulesWanted))
			})
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, rulesWanted int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "pc-profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			waitProfile(ns, rulesWanted)

			return ns
		}

		named := newNS(a, "named", 4)
		selected := newNS(a, "selected", 1)
		templates := newNS(a, "templates", 1)
		other := newNS(a, "other", 0)
		isolated := newNS(b, "named", 1)

		sequence := 0
		pod := func(ns *corev1.Namespace, class string) *corev1.Pod {
			sequence++

			return priorityClassPod(ns.Name, fmt.Sprintf("pod-%d", sequence), class)
		}
		create := func(actor client.Client, obj *corev1.Pod) *corev1.Pod {
			class := obj.Spec.PriorityClassName
			Eventually(func() error { return actor.Create(ctx, obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				g.Expect(stored.Spec.PriorityClassName).To(Equal(class))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

			return obj
		}
		deny := func(actor client.Client, obj client.Object, reasons ...string) {
			err := actor.Create(ctx, obj)
			Expect(err).To(HaveOccurred(), "an unauthorized create must never succeed")

			for _, reason := range reasons {
				Expect(err.Error()).To(ContainSubstring(reason))
			}

			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))).To(BeTrue())
		}
		expectEvents := func(obj client.Object, wanted ...string) {
			Eventually(func(g Gomega) {
				list := &eventsv1.EventList{}
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(obj.GetNamespace()))).To(Succeed())

				var reasons []string

				for _, event := range list.Items {
					if event.ReportingController == events.ReportingController && event.Regarding.Name == obj.GetName() && event.Regarding.Kind == "Pod" {
						g.Expect(event.Labels[meta.NewTenantLabel]).To(Equal(a.Name))

						reasons = append(reasons, event.Reason)
					}
				}

				g.Expect(reasons).To(ContainElements(wanted))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		By("preserving dry-run decisions without persisting Pods")
		dry := pod(named, silver)
		err := ownerA.Create(ctx, dry, client.DryRunAll)
		Expect(err).To(HaveOccurred(), "dry-run denial must preserve enforcement")
		Expect(err.Error()).To(ContainSubstring("is denied by namespace rule"))
		Expect(ownerA.Create(ctx, pod(named, gold), client.DryRunAll)).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(dry), dry))).To(BeTrue())

		By("allowing the last matching rule and auditing the decision")
		audited := create(ownerA, pod(named, gold))
		expectEvents(audited, events.ReasonNamespaceRuleAudit)

		By("rejecting classes the profile denies or does not list")
		denied := pod(named, bronze)
		deny(ownerA, denied, fmt.Sprintf("priority class %q at spec.priorityClassName is denied by namespace rule", bronze))
		expectEvents(denied, events.ReasonNamespaceRuleAudit, events.ReasonForbiddenPriorityClass)
		deny(ownerA, pod(named, silver), "is denied by namespace rule")

		By("applying different profiles within one tenant")
		create(ownerA, pod(selected, gold))
		deny(ownerA, pod(selected, silver), fmt.Sprintf("priority class %q at spec.priorityClassName is not allowed by namespace rule", silver), "Allowed priority classes")
		deny(ownerA, pod(selected, bronze), "is not allowed by namespace rule")
		create(ownerA, pod(other, silver))
		create(ownerA, pod(other, bronze))

		By("isolating another tenant from these rules")
		create(ownerB, pod(isolated, bronze))
		deny(ownerB, pod(isolated, gold), "is not allowed by namespace rule")

		By("using the destination namespace profile even with explicitly delegated access")
		cross := pod(isolated, bronze)
		crossErr := ownerA.Create(ctx, cross)
		Expect(apierrors.IsForbidden(crossErr)).To(BeTrue())
		Expect(crossErr).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), cross))).To(BeTrue())

		role := &rbacv1.Role{Name: "pod-writer", Namespace: isolated.Name, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"create"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())

		binding := &rbacv1.RoleBinding{Name: role.Name, Namespace: role.Namespace, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: a.Name}}}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		create(ownerA, pod(isolated, bronze))
		deny(ownerA, pod(isolated, gold), "is not allowed by namespace rule")

		By("checking the Pod template of an explicitly targeted controller")
		deployment := func(name, class string) *appsv1.Deployment {
			dep := MakeDeployment(templates.Name, name, 0, map[string]string{"env": "e2e"}, "")
			dep.Labels = map[string]string{"env": "e2e"}
			dep.Spec.Template.Spec.PriorityClassName = class

			return dep
		}
		deny(ownerA, deployment("denied", gold), "spec.template", fmt.Sprintf("priority class %q at spec.priorityClassName is not allowed by namespace rule", gold))
		deny(ownerA, pod(templates, gold), "is not allowed by namespace rule")
		// Its Pods would only get a class when they are created, so it must name one.
		deny(ownerA, deployment("classless", ""), "spec.template", "priority class is required at spec.priorityClassName because class rules target deployment")

		workload := deployment("allowed", silver)
		Eventually(func() error { return ownerA.Create(ctx, workload) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		// The other profiles do not target Deployments, so their templates are not checked.
		unchecked := deployment("unchecked", bronze)
		unchecked.Namespace = selected.Name
		Eventually(func() error { return ownerA.Create(ctx, unchecked) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(workload), current)).To(Succeed())
			current.Spec.Template.Spec.PriorityClassName = gold

			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("a forbidden template class was accepted")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("spec.template"), ContainSubstring("is not allowed by namespace rule"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error {
			current := &appsv1.Deployment{}
			if err := ownerA.Get(ctx, client.ObjectKeyFromObject(workload), current); err != nil {
				return err
			}

			current.Spec.Template.Labels["revision"] = "two"

			return ownerA.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(workload), stored)).To(Succeed())
			g.Expect(stored.Spec.Template.Spec.PriorityClassName).To(Equal(silver))
			g.Expect(stored.Spec.Template.Labels).To(HaveKeyWithValue("revision", "two"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting an empty matcher without changing the active policy")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			current.Spec.Rules[1].Enforce.Workloads.Classes.Priority = []rules.WorkloadClassMatch{{}}

			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("an empty priority class matcher was accepted")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("workloads.classes.priority[0]"), ContainSubstring("at least one of exact, exp, matchLabels or matchExpressions must be set"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules[1].Enforce.Workloads.Classes.Priority).To(Equal([]rules.WorkloadClassMatch{{Exact: []string{gold}}}))

		By("applying a changed rule to new Pods while existing Pods stay updatable next to a conditional rule")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}

			current.Spec.Rules[1].Enforce.Workloads.Classes.Priority = []rules.WorkloadClassMatch{{Exact: []string{silver}}}

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
		waitPriorityClassProfile(ctx, named, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
			g.Expect(bodies).To(HaveLen(4))
			g.Expect(bodies[1].Enforce.Workloads.Classes.Priority).To(Equal([]rules.WorkloadClassMatch{{Exact: []string{silver}}}))
		})
		create(ownerA, pod(named, silver))
		deny(ownerA, pod(named, gold), "is denied by namespace rule")
		Eventually(func() error {
			current := &corev1.Pod{}
			if err := ownerA.Get(ctx, client.ObjectKeyFromObject(audited), current); err != nil {
				return err
			}

			current.Labels["updated"] = "true"

			return ownerA.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(audited), stored)).To(Succeed())
			g.Expect(stored.Labels).To(HaveKeyWithValue("updated", "true"))
			g.Expect(stored.Spec.PriorityClassName).To(Equal(gold))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		create(ownerB, pod(isolated, bronze))

		By("following the labels of the PriorityClass")
		Eventually(func() error {
			current := &schedulingv1.PriorityClass{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: silver}, current); err != nil {
				return err
			}

			current.Labels["pc-tier"] = premium["pc-tier"]

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		create(ownerA, pod(selected, silver))
		Eventually(func() error {
			current := &schedulingv1.PriorityClass{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: gold}, current); err != nil {
				return err
			}

			delete(current.Labels, "pc-tier")

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		deny(ownerA, pod(selected, gold), "is not allowed by namespace rule")

		By("selecting a different namespace profile after a label change")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current); err != nil {
				return err
			}

			current.Labels["pc-profile"] = "selected"

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, 1)
		deny(ownerA, pod(other, bronze), "is not allowed by namespace rule")
		create(ownerA, pod(other, silver))
	})
})
