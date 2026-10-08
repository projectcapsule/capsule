// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("deprecated scheduler rules", Label("tenant", "rules", "enforce", "workloads", "scheduler"), func() {
	It("combines both fields, warns on writes, and preserves namespace profiles and tenant isolation", func() {
		ctx := context.Background()
		prefix := "e2e-scheduler-alias-" + rand.String(6)
		var warnings bytes.Buffer
		config := rest.CopyConfig(cfg)
		config.WarningHandler = rest.NewWarningWriter(&warnings, rest.WarningWriterOptions{})
		admin, err := client.New(config, client.Options{Scheme: k8sClient.Scheme()})
		Expect(err).NotTo(HaveOccurred())
		expectWarning := func() {
			Expect(warnings.String()).To(ContainSubstring("`spec.rules[].enforce.workloads.schedulers` is deprecated"))
			Expect(warnings.String()).To(ContainSubstring("`spec.rules[].enforce.workloads.placement.schedulers`"))
			warnings.Reset()
		}
		var tenants []*capsulev1beta2.Tenant
		var owners []client.Client
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			tnt := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"scheduler-profile": "selected"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
						Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
							Targets:    []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment},
							Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"legacy-" + suffix, "blocked"}}, {ExpressionRegex: apiruntime.ExpressionRegex{Expression: "^regex-" + suffix + "$"}}},
							Placement:  rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"preferred-" + suffix}}}},
						}},
					}},
					{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
						Targets:   []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment},
						Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"blocked"}}}},
					}}}},
				},
			}}
			if suffix == "b" {
				tnt.Spec.Rules[0].Enforce.Workloads.Placement.Schedulers = nil
			}
			Expect(admin.Create(ctx, tnt)).To(Succeed())
			expectWarning()
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		expectProfile := func(ns *corev1.Namespace, count int, legacy string) {
			Eventually(func(g Gomega) {
				status := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(status.Status.Rules).To(HaveLen(count))
				if count == 2 {
					if legacy == "" {
						g.Expect(status.Status.Rules[0].Enforce.Workloads.Schedulers).To(BeEmpty())
					} else {
						g.Expect(status.Status.Rules[0].Enforce.Workloads.Schedulers[0].Exact[0]).To(Equal(legacy))
					}
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNamespace := func(tenant int, selected bool) *corev1.Namespace {
			labels := map[string]string{meta.TenantLabel: tenants[tenant].Name}
			if selected {
				labels["scheduler-profile"] = "selected"
			}
			ns := NewNamespace("", labels)
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			return ns
		}
		selected, unselected, otherTenant := newNamespace(0, true), newNamespace(0, false), newNamespace(1, true)
		TenantNamespaceReady(tenants[0], selected, 2)
		TenantNamespaceReady(tenants[0], unselected, 2)
		TenantNamespaceReady(tenants[1], otherTenant, 1)
		expectProfile(selected, 2, "legacy-a")
		expectProfile(unselected, 1, "")
		expectProfile(otherTenant, 2, "legacy-b")
		podFor := func(ns *corev1.Namespace, scheduler string) *corev1.Pod {
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "scheduler-" + rand.String(8), Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: corev1.PodSpec{
				SchedulerName: scheduler, SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/e2e-scheduler"}},
				SecurityContext: nobodyPodSecurityContext(), Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
			}}
		}
		checkPod := func(owner client.Client, ns *corev1.Namespace, scheduler string, allowed bool) {
			pod := podFor(ns, scheduler)
			err := owner.Create(ctx, pod)
			if allowed {
				Expect(err).NotTo(HaveOccurred())
				Eventually(func(g Gomega) {
					stored := &corev1.Pod{}
					g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
					g.Expect(stored.Spec.SchedulerName).To(Equal(scheduler))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			} else {
				Expect(err).To(MatchError(And(ContainSubstring("scheduler"), ContainSubstring("spec.schedulerName"))))
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
			}
		}
		By("enforcing the union, later deny rules, namespace selection, and tenant isolation")
		checkPod(owners[0], selected, "legacy-a", true)
		checkPod(owners[0], selected, "regex-a", true)
		checkPod(owners[0], selected, "preferred-a", true)
		checkPod(owners[0], selected, "unlisted", false)
		checkPod(owners[0], selected, "blocked", false)
		checkPod(owners[0], unselected, "unlisted", true)
		checkPod(owners[1], otherTenant, "legacy-b", true)
		checkPod(owners[1], otherTenant, "legacy-a", false)
		checkPod(owners[1], otherTenant, "regex-a", false)
		cross := podFor(otherTenant, "legacy-b")
		err = owners[0].Create(ctx, cross)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.Pod{}))).To(BeTrue())

		By("enforcing the deprecated scheduler on controller templates and their updates")
		template := podFor(selected, "legacy-a")
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "scheduler-template", Namespace: selected.Name, Labels: map[string]string{"env": "e2e"}}, Spec: appsv1.DeploymentSpec{
			Replicas: new(int32(0)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "scheduler-template"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "scheduler-template"}}, Spec: template.Spec},
		}}
		Expect(owners[0].Create(ctx, deployment)).To(Succeed())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(deployment), current)).To(Succeed())
			g.Expect(current.Spec.Template.Spec.SchedulerName).To(Equal("legacy-a"))
			current.Spec.Template.Spec.SchedulerName = "unlisted"
			err := owners[0].Update(ctx, current)
			if err == nil {
				Fail("unlisted scheduler was accepted in a Deployment")
			}
			g.Expect(apierrors.IsForbidden(err)).To(BeTrue())
			g.Expect(err).To(MatchError(ContainSubstring(`spec.template: scheduler "unlisted" at spec.schedulerName is not allowed by namespace rule`)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.SchedulerName).To(Equal("legacy-a"))

		By("rejecting malformed legacy matchers without changing the persisted policy")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(admin.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Enforce.Workloads.Schedulers[0].Expression = "["
			err := admin.Update(ctx, current)
			if err == nil {
				Fail("invalid legacy scheduler expression was accepted")
			}
			g.Expect(err).To(MatchError(ContainSubstring(`workloads.schedulers[0].exp "[" is invalid`)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		stored := &capsulev1beta2.Tenant{}
		Expect(admin.Get(ctx, client.ObjectKeyFromObject(tenants[0]), stored)).To(Succeed())
		Expect(stored.Spec.Rules[0].Enforce.Workloads.Schedulers[0].Expression).To(BeEmpty())

		By("reconciling legacy policy and namespace label updates")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			current.Spec.Rules[0].Enforce.Workloads.Schedulers[0].Exact[0] = "updated-a"
			return admin.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectWarning()
		expectProfile(selected, 2, "updated-a")
		checkPod(owners[0], selected, "updated-a", true)
		checkPod(owners[0], selected, "legacy-a", false)
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(unselected), current); err != nil {
				return err
			}
			current.Labels["scheduler-profile"] = "selected"
			return admin.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectProfile(unselected, 2, "updated-a")
		checkPod(owners[0], unselected, "unlisted", false)
		checkPod(owners[1], otherTenant, "legacy-b", true)

		By("removing the warning after migration while preserving both matcher lists")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := admin.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			workloads := &current.Spec.Rules[0].Enforce.Workloads
			workloads.Placement.Schedulers = append(workloads.Placement.Schedulers, workloads.Schedulers...)
			workloads.Schedulers = nil
			return admin.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(warnings.String()).To(BeEmpty())
		expectProfile(selected, 2, "")
		checkPod(owners[0], selected, "updated-a", true)
		checkPod(owners[0], selected, "preferred-a", true)
		checkPod(owners[0], selected, "unlisted", false)
	})
})
