// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("workload template targets", Label("tenant", "rules", "workloads", "workload-targets"), func() {
	It("checks controller templates and defaults resources only in selected namespace profiles", func() {
		ctx := context.Background()
		prefix := "e2e-targets-" + rand.String(8)
		a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"profile": "templates"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
			Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateCronJob}, Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"forbidden-scheduler"}}},

				NodeSelector: []rules.WorkloadNodeSelectorMatch{{Key: &rules.PlacementExpressionMatch{Exact: []string{"example.com/forbidden"}}}}}, Registries: []rules.OCIRegistry{{ExpressionMatch: apiruntime.ExpressionMatch{Exact: []string{"example.com/blocked/app:v1"}}}},

			Resources: &rules.WorkloadResourceRules{Requests: map[corev1.ResourceName]rules.WorkloadResourceRequestPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceRequestPolicyDefault, Value: new(resource.MustParse("32Mi"))}}, Limits: map[corev1.ResourceName]rules.WorkloadResourceLimitPolicy{corev1.ResourceMemory: {Policy: rules.WorkloadResourceLimitPolicyRatio, Value: new(resource.MustParse("2"))}}},
		}}}}}}}
		b := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		ownerA, ownerB := impersonationClient(a.Name, withDefaultGroups(nil)), impersonationClient(b.Name, withDefaultGroups(nil))
		waitProfile := func(ns *corev1.Namespace, selected, conditional bool) {
			Eventually(func(g Gomega) {
				rs := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, rs)).To(Succeed())
				g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
				if !selected {
					g.Expect(rs.Status.Rules).To(BeEmpty())
					return
				}
				g.Expect(rs.Status.Rules).To(HaveLen(1))
				g.Expect(rs.Status.Rules[0].Enforce.Workloads.Targets).To(Equal([]rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateCronJob}))
				if conditional {
					g.Expect(rs.Status.Rules[0].Enforce.Conditions).To(Equal([]rules.AdmissionCondition{{Expression: "false"}}))
				} else {
					g.Expect(rs.Status.Rules[0].Enforce.Conditions).To(BeEmpty())
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, selected bool) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			waitProfile(ns, selected, false)
			return ns
		}
		selected, other, isolated := newNS(a, "templates", true), newNS(a, "other", false), newNS(b, "templates", false)
		deployment := func(ns *corev1.Namespace, name string) *appsv1.Deployment {
			obj := MakeDeployment(ns.Name, name, 0, map[string]string{"env": "e2e"}, "")
			obj.Labels = map[string]string{"env": "e2e"}
			return obj
		}
		By("persisting resource defaults in an explicitly targeted Deployment template")
		good := deployment(selected, "good")
		Expect(ownerA.Create(ctx, good)).To(Succeed())
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(good), stored)).To(Succeed())
			g.Expect(stored.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String()).To(Equal("32Mi"))
			g.Expect(stored.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String()).To(Equal("64Mi"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("rejecting configured property violations rather than every selected Deployment")
		for _, tc := range []struct {
			name, reason string
			change       func(*appsv1.Deployment)
		}{
			{"scheduler", "scheduler", func(d *appsv1.Deployment) { d.Spec.Template.Spec.SchedulerName = "forbidden-scheduler" }},
			{"registry", "registry", func(d *appsv1.Deployment) { d.Spec.Template.Spec.Containers[0].Image = "example.com/blocked/app:v1" }},
			{"placement", "nodeSelector", func(d *appsv1.Deployment) {
				d.Spec.Template.Spec.NodeSelector = map[string]string{"example.com/forbidden": "yes"}
			}},
			{"resources", "resource", func(d *appsv1.Deployment) {
				d.Spec.Template.Spec.Containers[0].Resources = corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("32Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("96Mi")}}
			}},
		} {
			obj := deployment(selected, tc.name)
			tc.change(obj)
			Expect(ownerA.Create(ctx, obj)).To(MatchError(And(ContainSubstring("spec.template"), ContainSubstring(tc.reason))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &appsv1.Deployment{}))).To(BeTrue())
			for _, outside := range []struct {
				ns    *corev1.Namespace
				actor client.Client
			}{{other, ownerA}, {isolated, ownerB}} {
				obj := deployment(outside.ns, tc.name)
				tc.change(obj)
				originalResources := obj.Spec.Template.Spec.Containers[0].Resources.DeepCopy()
				Expect(outside.actor.Create(ctx, obj)).To(Succeed())
				Eventually(func(g Gomega) {
					stored := &appsv1.Deployment{}
					g.Expect(outside.actor.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
					g.Expect(stored.Spec.Template.Spec.Containers[0].Resources).To(Equal(*originalResources))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		}
		By("using the CronJob's nested Pod template")
		cron := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: "cron", Namespace: selected.Name, Labels: map[string]string{"env": "e2e"}}, Spec: batchv1.CronJobSpec{Schedule: "0 0 * * *", Suspend: new(true), JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{Template: deployment(selected, "cron").Spec.Template}}}}
		cron.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
		Expect(ownerA.Create(ctx, cron)).To(Succeed())
		Eventually(func(g Gomega) {
			stored := &batchv1.CronJob{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(cron), stored)).To(Succeed())
			g.Expect(stored.Spec.JobTemplate.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().String()).To(Equal("32Mi"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("rejecting a controller template update and preserving the stored spec")
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(good), stored)).To(Succeed())
			stored.Spec.Template.Spec.Containers[0].Image = "example.com/blocked/app:v1"
			err := ownerA.Update(ctx, stored)
			if err == nil {
				Fail("forbidden template update succeeded")
			}
			g.Expect(err).To(MatchError(ContainSubstring("registry")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(good), stored)).To(Succeed())
			g.Expect(stored.Spec.Template.Spec.Containers[0].Image).NotTo(Equal("example.com/blocked/app:v1"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("applying the profile after an administrator changes a namespace label")
		Eventually(func(g Gomega) {
			ns := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(other), ns)).To(Succeed())
			ns.Labels["profile"] = "templates"
			g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, true, false)
		blocked := deployment(other, "newly-blocked")
		blocked.Spec.Template.Spec.SchedulerName = "forbidden-scheduler"
		Expect(ownerA.Create(ctx, blocked)).To(MatchError(ContainSubstring("scheduler")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(blocked), &appsv1.Deployment{}))).To(BeTrue())
		By("re-evaluating enforcement conditions after a policy update")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
			current.Spec.Rules[0].Enforce.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(selected, true, true)
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(good), stored)).To(Succeed())
			stored.Spec.Template.Spec.Containers[0].Image = "example.com/blocked/app:v1"
			g.Expect(ownerA.Update(ctx, stored)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(good), stored)).To(Succeed())
			g.Expect(stored.Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/blocked/app:v1"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("preserving cross-tenant authorization")
		cross := deployment(isolated, "cross-tenant")
		Expect(ownerA.Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource \"deployments\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &appsv1.Deployment{}))).To(BeTrue())
	})
})
