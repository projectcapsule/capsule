// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	nodev1 "k8s.io/api/node/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// runtimeClassPod never starts: the scheduling gate keeps it Pending, so the
// spec only exercises admission and no node needs the handler of its class.
func runtimeClassPod(namespace, name, class string) *corev1.Pod {
	pod := &corev1.Pod{
		Name: name, Namespace: namespace, Labels: map[string]string{"env": "e2e"},
		Spec: corev1.PodSpec{
			SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/e2e-runtime-class"}},
			SecurityContext: nobodyPodSecurityContext(),
			Containers:      []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
		},
	}

	if class != "" {
		pod.Spec.RuntimeClassName = &class
	}

	return pod
}

var _ = Describe("RuntimeClass namespace profiles", Label("tenant", "rules", "workloads", "runtimeclass", "runtime-class-rules"), func() {
	It("enforces composed profiles, changes, and tenant boundaries", func() {
		ctx := context.Background()
		prefix := "e2e-rc-rules-" + rand.String(8)
		sandboxed, standard, plain := prefix+"-sandboxed", prefix+"-standard", prefix+"-plain"
		isolated := map[string]string{"rc-tier": prefix + "-isolated"}

		// The classes carry no environment label: the legacy RuntimeClass specs
		// sweep the classes that do, and they run in parallel.
		for name, labels := range map[string]map[string]string{sandboxed: isolated, standard: {"rc-tier": prefix + "-standard"}, plain: nil} {
			class := &nodev1.RuntimeClass{Name: name, Labels: mergeMaps(map[string]string{"env": "e2e"}, labels), Handler: "e2e-runtime-class"}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(DeleteInBackground, class)
		}

		policy := func(profile string, action rules.ActionType, targets []rules.WorkloadValidationTarget, matches ...rules.WorkloadClassMatch) *rules.NamespaceRuleBodyTenant {
			body := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action:    action,
				Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: targets, Classes: rules.WorkloadClassEnforcement{Runtime: matches}},
			}}}
			if profile != "" {
				body.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"rc-profile": profile}}
			}

			return body
		}

		a := &capsulev1beta2.Tenant{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{
			policy("named", rules.ActionTypeDeny, nil, rules.WorkloadClassMatch{Expression: "^" + prefix + "-"}),
			policy("named", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{Exact: []string{sandboxed}}),
			policy("named", rules.ActionTypeAudit, nil, rules.WorkloadClassMatch{Exact: []string{sandboxed, plain}}),
			policy("selected", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{MatchLabels: isolated}),
			policy("templates", rules.ActionTypeAllow, []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment}, rules.WorkloadClassMatch{Exact: []string{standard}}),
			// An unrelated conditional rule that matches every Pod of this spec: it must
			// not make updates check a class they keep.
			{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"rc-profile": "named"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action:     rules.ActionTypeDeny,
				Conditions: []rules.AdmissionCondition{{Name: "e2e-pods", Expression: `has(object.metadata.labels) && 'env' in object.metadata.labels && object.metadata.labels['env'] == 'e2e'`}},
				Workloads:  rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{prefix + "-blocked-scheduler"}}}}},
			}}},
		}}}
		b := &capsulev1beta2.Tenant{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}, Rules: []*rules.NamespaceRuleBodyTenant{
			policy("", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{Exact: []string{plain}}),
		}}}
		// The legacy Tenant policy admits fewer classes than the rule of this tenant.
		// It needs the selector: a legacy list of names alone matches every class.
		legacy := &capsulev1beta2.Tenant{Name: prefix + "-legacy", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners:         rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-legacy"}},
			RuntimeClasses: &api.DefaultAllowedListSpec{MatchLabels: isolated},
			Rules: []*rules.NamespaceRuleBodyTenant{
				policy("", rules.ActionTypeAllow, nil, rules.WorkloadClassMatch{Exact: []string{sandboxed, standard}}),
			},
		}}

		for _, tnt := range []*capsulev1beta2.Tenant{a, b, legacy} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		ownerA := impersonationClient(a.Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Name, withDefaultGroups(nil))
		ownerLegacy := impersonationClient(legacy.Name, withDefaultGroups(nil))

		waitProfile := func(ns *corev1.Namespace, rulesWanted int) {
			waitWorkloadClassProfile(ctx, ns, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
				g.Expect(bodies).To(HaveLen(rulesWanted))
			})
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, rulesWanted int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "rc-profile": profile})
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
		isolatedNS := newNS(b, "named", 1)
		legacyNS := newNS(legacy, "named", 1)

		sequence := 0
		pod := func(ns *corev1.Namespace, class string) *corev1.Pod {
			sequence++

			return runtimeClassPod(ns.Name, fmt.Sprintf("pod-%d", sequence), class)
		}
		create := func(actor client.Client, obj *corev1.Pod) *corev1.Pod {
			class := obj.Spec.RuntimeClassName
			Eventually(func() error { return actor.Create(ctx, obj) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				g.Expect(stored.Spec.RuntimeClassName).To(Equal(class))
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

		By("preserving dry-run decisions without persisting Pods or Events")
		dry := pod(named, standard)
		err := ownerA.Create(ctx, dry, client.DryRunAll)
		Expect(err).To(HaveOccurred(), "dry-run denial must preserve enforcement")
		Expect(err.Error()).To(ContainSubstring("is denied by namespace rule"))
		dryAudited := pod(named, sandboxed)
		Expect(ownerA.Create(ctx, dryAudited, client.DryRunAll)).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(dry), dry))).To(BeTrue())

		By("allowing the last matching rule and auditing the decision")
		audited := create(ownerA, pod(named, sandboxed))
		expectEvents(audited, events.ReasonNamespaceRuleAudit)

		// The audit of the live Pod arrived, so the dry runs above have been
		// processed: neither the denied nor the audited one left an Event.
		dryEvents := &eventsv1.EventList{}
		Expect(k8sClient.List(ctx, dryEvents, client.InNamespace(named.Name))).To(Succeed())
		for _, event := range dryEvents.Items {
			Expect(event.Regarding.Name).NotTo(BeElementOf(dry.Name, dryAudited.Name))
		}

		By("rejecting classes the profile denies or does not list")
		denied := pod(named, plain)
		deny(ownerA, denied, fmt.Sprintf("runtime class %q at spec.runtimeClassName is denied by namespace rule", plain))
		expectEvents(denied, events.ReasonNamespaceRuleAudit, events.ReasonForbiddenRuntimeClass)
		deny(ownerA, pod(named, standard), "is denied by namespace rule")

		By("applying different profiles within one tenant")
		create(ownerA, pod(selected, sandboxed))
		deny(ownerA, pod(selected, standard), fmt.Sprintf("runtime class %q at spec.runtimeClassName is not allowed by namespace rule", standard), "Allowed runtime classes")
		deny(ownerA, pod(selected, plain), "is not allowed by namespace rule")
		create(ownerA, pod(other, standard))
		create(ownerA, pod(other, plain))

		By("leaving Pods without a class to the default runtime in every profile")
		for _, ns := range []*corev1.Namespace{named, selected, templates, other} {
			create(ownerA, pod(ns, ""))
		}

		By("isolating another tenant from these rules")
		create(ownerB, pod(isolatedNS, plain))
		deny(ownerB, pod(isolatedNS, sandboxed), "is not allowed by namespace rule")

		By("using the destination namespace profile even with explicitly delegated access")
		cross := pod(isolatedNS, plain)
		crossErr := ownerA.Create(ctx, cross)
		Expect(apierrors.IsForbidden(crossErr)).To(BeTrue())
		Expect(crossErr).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), cross))).To(BeTrue())

		role := &rbacv1.Role{Name: "pod-writer", Namespace: isolatedNS.Name, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"pods"}, Verbs: []string{"create"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())

		binding := &rbacv1.RoleBinding{Name: role.Name, Namespace: role.Namespace, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: a.Name}}}
		Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		create(ownerA, pod(isolatedNS, plain))
		deny(ownerA, pod(isolatedNS, sandboxed), "is not allowed by namespace rule")

		By("keeping the legacy Tenant policy as an additional boundary")
		create(ownerLegacy, pod(legacyNS, sandboxed))
		deny(ownerLegacy, pod(legacyNS, standard), "is forbidden for the current Tenant")
		deny(ownerLegacy, pod(legacyNS, plain))
		create(ownerLegacy, pod(legacyNS, ""))

		By("no longer listing the available classes in the tenant status")
		for _, tnt := range []*capsulev1beta2.Tenant{a, legacy} {
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)

			latest := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), latest)).To(Succeed())
			Expect(latest.Status.Classes.RuntimeClasses).To(BeEmpty())

			encoded, err := json.Marshal(latest.Status.Classes)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(encoded)).NotTo(ContainSubstring(`"runtime"`))
		}

		By("checking the Pod template of an explicitly targeted controller")
		deployment := func(name, class string) *appsv1.Deployment {
			dep := MakeDeployment(templates.Name, name, 0, map[string]string{"env": "e2e"}, "")
			dep.Labels = map[string]string{"env": "e2e"}

			if class != "" {
				dep.Spec.Template.Spec.RuntimeClassName = &class
			}

			return dep
		}
		deny(ownerA, deployment("denied", sandboxed), "spec.template", fmt.Sprintf("runtime class %q at spec.runtimeClassName is not allowed by namespace rule", sandboxed))
		deny(ownerA, pod(templates, sandboxed), "is not allowed by namespace rule")

		// Nothing assigns a runtime class to its Pods later, so a template may omit it.
		classless := deployment("classless", "")
		Eventually(func() error { return ownerA.Create(ctx, classless) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		workload := deployment("allowed", standard)
		Eventually(func() error { return ownerA.Create(ctx, workload) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		// The other profiles do not target Deployments, so their templates are not checked.
		unchecked := deployment("unchecked", plain)
		unchecked.Namespace = selected.Name
		Eventually(func() error { return ownerA.Create(ctx, unchecked) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		for _, target := range []*appsv1.Deployment{workload, classless} {
			Eventually(func(g Gomega) {
				current := &appsv1.Deployment{}
				g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(target), current)).To(Succeed())
				current.Spec.Template.Spec.RuntimeClassName = &sandboxed

				err := ownerA.Update(ctx, current)
				if err == nil {
					Fail("a forbidden template class was accepted")
				}

				g.Expect(err).To(MatchError(And(ContainSubstring("spec.template"), ContainSubstring("is not allowed by namespace rule"))))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

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
			g.Expect(stored.Spec.Template.Spec.RuntimeClassName).To(HaveValue(Equal(standard)))
			g.Expect(stored.Spec.Template.Labels).To(HaveKeyWithValue("revision", "two"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		stored := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(classless), stored)).To(Succeed())
		Expect(stored.Spec.Template.Spec.RuntimeClassName).To(BeNil())

		By("rejecting an empty matcher without changing the active policy")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			current.Spec.Rules[1].Enforce.Workloads.Classes.Runtime = []rules.WorkloadClassMatch{{}}

			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("an empty runtime class matcher was accepted")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("workloads.classes.runtime[0]"), ContainSubstring("at least one of exact, exp, matchLabels or matchExpressions must be set"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules[1].Enforce.Workloads.Classes.Runtime).To(Equal([]rules.WorkloadClassMatch{{Exact: []string{sandboxed}}}))

		By("applying a changed rule to new Pods while existing Pods stay updatable next to a conditional rule")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}

			current.Spec.Rules[1].Enforce.Workloads.Classes.Runtime = []rules.WorkloadClassMatch{{Exact: []string{standard}}}

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
		waitWorkloadClassProfile(ctx, named, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
			g.Expect(bodies).To(HaveLen(4))
			g.Expect(bodies[1].Enforce.Workloads.Classes.Runtime).To(Equal([]rules.WorkloadClassMatch{{Exact: []string{standard}}}))
		})
		create(ownerA, pod(named, standard))
		deny(ownerA, pod(named, sandboxed), "is denied by namespace rule")
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
			g.Expect(stored.Spec.RuntimeClassName).To(HaveValue(Equal(sandboxed)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		create(ownerB, pod(isolatedNS, plain))

		By("following the labels of the RuntimeClass")
		Eventually(func() error {
			current := &nodev1.RuntimeClass{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: standard}, current); err != nil {
				return err
			}

			current.Labels["rc-tier"] = isolated["rc-tier"]

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		create(ownerA, pod(selected, standard))
		Eventually(func() error {
			current := &nodev1.RuntimeClass{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: sandboxed}, current); err != nil {
				return err
			}

			delete(current.Labels, "rc-tier")

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		deny(ownerA, pod(selected, sandboxed), "is not allowed by namespace rule")

		By("selecting a different namespace profile after a label change")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current); err != nil {
				return err
			}

			current.Labels["rc-profile"] = "selected"

			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, 1)
		deny(ownerA, pod(other, plain), "is not allowed by namespace rule")
		create(ownerA, pod(other, standard))
	})
})
