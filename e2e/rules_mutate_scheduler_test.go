// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("scheduler mutation namespace profiles", Label("tenant", "rules", "workloads", "scheduler", "mutation"), func() {
	It("defaults or replaces schedulers per namespace while retaining enforcement and tenant isolation", func() {
		ctx := context.Background()
		prefix := "e2e-scheduler-" + rand.String(8)
		var tenants []*capsulev1beta2.Tenant
		var owners []client.Client
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			selected := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "scheduler-profile", Operator: metav1.LabelSelectorOpIn, Values: []string{"merge", "replace"}}}}
			tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					{NamespaceSelector: selected, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{
						{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Name: "default-scheduler", Expression: `!has(object.spec.schedulerName) || object.spec.schedulerName in ['', 'default-scheduler']`}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "{{ .tenant.metadata.name }}"}}},
						{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{NodeSelector: map[string]string{"scheduler.example.com/pool": name}}}},
						{Action: rules.MutationActionReplace, Conditions: []rules.AdmissionCondition{{Expression: `has(object.metadata.labels) && 'deny-scheduler' in object.metadata.labels && object.metadata.labels['deny-scheduler'] == 'true'`}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "forbidden-scheduler"}}},
					}}},
					{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"scheduler-profile": "replace"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: name + "-forced"}}}}}},
					{NamespaceSelector: selected, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "not-the-scheduler-owner"}}, Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "wrong-audience"}}}}}},
					{NamespaceSelector: selected, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"forbidden-scheduler"}}}}}}}},
					{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"scheduler-profile": "merge-only"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "{{ .tenant.metadata.name }}"}}}}}},
				},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}

		expectProfile := func(ns *corev1.Namespace, count int, scheduler string) {
			Eventually(func(g Gomega) {
				status := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(status.Status.Rules).To(HaveLen(count))
				if count > 0 {
					g.Expect(status.Status.Rules[0].Mutate[0].Workloads.Placement.Scheduler).To(Equal(scheduler))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNamespace := func(tenant int, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[tenant].Name, "scheduler-profile": profile})
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			count := 0
			if profile == "merge" {
				count = 3
			} else if profile == "replace" {
				count = 4
			} else if profile == "merge-only" {
				count = 1
			}
			expectProfile(ns, count, tenants[tenant].Name)
			return ns
		}
		mergeNS, replaceNS := newNamespace(0, "merge"), newNamespace(0, "replace")
		otherNS, tenantBNS := newNamespace(0, "other"), newNamespace(1, "merge")
		mergeOnlyNS := newNamespace(0, "merge-only")
		newPod := func(ns *corev1.Namespace, name, scheduler string) *corev1.Pod {
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: corev1.PodSpec{
				SchedulerName: scheduler, SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/e2e-scheduler"}},
				SecurityContext: nobodyPodSecurityContext(), Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
			}}
		}
		expectScheduler := func(pod *corev1.Pod, scheduler string) {
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
				g.Expect(stored.Spec.SchedulerName).To(Equal(scheduler))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		createPod := func(owner client.Client, ns *corev1.Namespace, name, scheduler, want string) *corev1.Pod {
			pod := newPod(ns, name, scheduler)
			Eventually(func() error { return owner.Create(ctx, pod) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			expectScheduler(pod, want)
			return pod
		}
		By("applying conditional defaults, preserving custom names, and replacing explicit schedulers")
		original := createPod(owners[0], mergeNS, "omitted", "", tenants[0].Name)
		createPod(owners[0], mergeNS, "explicit-default", corev1.DefaultSchedulerName, tenants[0].Name)
		createPod(owners[0], mergeNS, "custom", "custom-scheduler", "custom-scheduler")
		createPod(owners[0], replaceNS, "replace", "custom-scheduler", tenants[0].Name+"-forced")
		createPod(owners[0], otherNS, "unselected", "", corev1.DefaultSchedulerName)
		createPod(owners[1], tenantBNS, "tenant-b", "", tenants[1].Name)
		createPod(owners[0], mergeOnlyNS, "merge-retains-default", "", corev1.DefaultSchedulerName)
		createPod(owners[0], mergeOnlyNS, "merge-retains-custom", "custom-scheduler", "custom-scheduler")
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(original), stored)).To(Succeed())
			g.Expect(stored.Spec.NodeSelector).To(HaveKeyWithValue("scheduler.example.com/pool", tenants[0].Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("enforcing the final scheduler for both supplied and mutated names")
		for _, mutated := range []bool{false, true} {
			pod := newPod(mergeNS, "denied-explicit", "forbidden-scheduler")
			if mutated {
				pod.Name, pod.Spec.SchedulerName = "denied-mutation", ""
				pod.Labels["deny-scheduler"] = "true"
			}
			err := owners[0].Create(ctx, pod)
			Expect(err).To(MatchError(And(ContainSubstring("forbidden-scheduler"), ContainSubstring("spec.schedulerName"), ContainSubstring("denied"))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		}
		By("rejecting cross-tenant Pod creation through RBAC")
		crossTenant := newPod(tenantBNS, "cross-tenant", "")
		err := owners[0].Create(ctx, crossTenant)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), &corev1.Pod{}))).To(BeTrue())

		By("rejecting blank scheduler configuration without changing the active policy")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[0].Workloads.Placement.Scheduler = "   "
			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("blank scheduler configuration was accepted")
			}
			g.Expect(err).To(MatchError(And(ContainSubstring("workloads.placement.scheduler"), ContainSubstring("must not be blank"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedTenant := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules[0].Mutate[0].Workloads.Placement.Scheduler).To(Equal("{{ .tenant.metadata.name }}"))

		By("applying a changed policy only to newly created Pods")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			current.Spec.Rules[0].Mutate[0].Workloads.Placement.Scheduler = "updated-scheduler"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(tenants[0], metav1.ConditionTrue, defaultTimeoutInterval)
		expectProfile(mergeNS, 3, "updated-scheduler")
		createPod(owners[0], mergeNS, "updated-policy", "", "updated-scheduler")
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
			g.Expect(stored.Spec.SchedulerName).To(Equal(tenants[0].Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		createPod(owners[1], tenantBNS, "tenant-b-unchanged", "", tenants[1].Name)

		By("selecting a different namespace profile after a label change")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(otherNS), current); err != nil {
				return err
			}
			current.Labels["scheduler-profile"] = "merge"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectProfile(otherNS, 3, "updated-scheduler")
		createPod(owners[0], otherNS, "selected-now", "", "updated-scheduler")
	})
})
