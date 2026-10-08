// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	networkingv1 "k8s.io/api/networking/v1"
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

var _ = Describe("dry-run rule Events", Label("tenant", "rules", "enforce", "dry-run-events"), func() {
	It("preserves composed decisions and live Events across namespace profiles and tenants", func() {
		ctx := context.Background()
		prefix := "e2e-dry-events-" + rand.String(8)
		profileRule := func(action rules.ActionType) *rules.NamespaceRuleBodyTenant {
			return &rules.NamespaceRuleBodyTenant{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"event-profile": string(action)}},
				NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
					Action:     action,
					Conditions: []rules.AdmissionCondition{{Expression: `has(object.metadata.labels) && 'event-trigger' in object.metadata.labels`}},
					Metadata:   []rules.MetadataRule{{VersionKinds: apiruntime.VersionKinds{APIGroups: []string{"networking.k8s.io/v1"}, Kinds: []string{"NetworkPolicy"}}, Labels: map[string]rules.MetadataValueRule{"event-trigger": {}}}},
					Network:    rules.NamespaceRuleEnforceNetworkBody{Policies: rules.NamespaceRuleEnforceNetworkPoliciesBody{Egress: &rules.NetworkPolicyCIDRRule{CIDRs: []string{"10.0.0.0/8"}}}},
					Services:   rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeExternalName}},
					Workloads:  rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod, rules.ValidateDeployment}, Placement: rules.WorkloadPlacementEnforcement{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"event-scheduler"}}}}},
					Ingress:    rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeIngress}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"event.example.com"}}}},
				}},
			}
		}
		a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{profileRule(rules.ActionTypeAudit), profileRule(rules.ActionTypeDeny)}}}
		b := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}, Rules: []*rules.NamespaceRuleBodyTenant{profileRule(rules.ActionTypeAudit)}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		ownerA, ownerB := impersonationClient(a.Name, withDefaultGroups(nil)), impersonationClient(b.Name, withDefaultGroups(nil))
		waitRules := func(ns *corev1.Namespace, want ...rules.ActionType) {
			Eventually(func(g Gomega) {
				rs := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: meta.NameForManagedRuleStatus(), Namespace: ns.Name}, rs)).To(Succeed())
				g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
				ready := rs.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				var got []rules.ActionType
				for _, body := range rs.Status.Rules {
					if body.Enforce != nil {
						got = append(got, body.Enforce.Action)
					}
				}
				g.Expect(got).To(Equal(want))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, count uint, want ...rules.ActionType) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "event-profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceReady(tnt, ns, count)
			waitRules(ns, want...)
			return ns
		}
		audit := newNS(a, "audit", 1, rules.ActionTypeAudit)
		deny := newNS(a, "deny", 2, rules.ActionTypeDeny)
		other := newNS(a, "other", 3)
		isolated := newNS(b, "audit", 1, rules.ActionTypeAudit)
		podSpec := corev1.PodSpec{SchedulerName: "event-scheduler", Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10"}}}
		cases := []struct {
			kind, denial, reason string
			object               client.Object
		}{
			{"networkpolicy", "metadata label", events.ReasonForbiddenMetadata, &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: "10.20.0.0/16"}}}}}}}},
			{"service", "service type", events.ReasonForbiddenServiceType, &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "event.example.com"}}},
			{"pod", "scheduler", events.ReasonForbiddenPodScheduler, &corev1.Pod{Spec: podSpec}},
			{"deployment", "scheduler", events.ReasonForbiddenPodScheduler, &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: new(int32(0)), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "event"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "event"}}, Spec: podSpec}}}},
			{"ingress", "ingress hostname", events.ReasonForbiddenIngressHostname, &networkingv1.Ingress{Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{Host: "event.example.com", IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/", PathType: new(networkingv1.PathTypePrefix), Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "backend", Port: networkingv1.ServiceBackendPort{Number: 80}}}}}}}}}}}},
		}
		candidate := func(template client.Object, ns, name string, trigger bool) client.Object {
			obj := template.DeepCopyObject().(client.Object)
			obj.SetName(name)
			obj.SetNamespace(ns)
			obj.SetLabels(map[string]string{"env": "e2e"})
			if trigger {
				obj.GetLabels()["event-trigger"] = "yes"
			}
			return obj
		}
		checkResult := func(g Gomega, err error, denied bool, reason string) {
			if denied {
				if err == nil {
					Fail("disallowed operation succeeded")
				}
				g.Expect(err).To(MatchError(ContainSubstring(reason)))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		}
		By("evaluating dry-run CREATE and UPDATE without persisting objects, changes or Events")
		start := time.Now()
		for _, ns := range []*corev1.Namespace{audit, deny} {
			for _, tc := range cases {
				dry := candidate(tc.object, ns.Name, "dry-create-"+tc.kind, true)
				checkResult(Default, ownerA.Create(ctx, dry, client.DryRunAll), ns == deny, tc.denial)
				Expect(apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(dry), tc.object.DeepCopyObject().(client.Object)))).To(BeTrue())
				base := candidate(tc.object, ns.Name, "update-"+tc.kind, false)
				Expect(ownerA.Create(ctx, base)).To(Succeed())
				Eventually(func(g Gomega) {
					current := tc.object.DeepCopyObject().(client.Object)
					g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(base), current)).To(Succeed())
					current.GetLabels()["event-trigger"] = "yes"
					err := ownerA.Update(ctx, current, client.DryRunAll)
					if apierrors.IsConflict(err) {
						g.Expect(err).NotTo(HaveOccurred())
						return
					}
					checkResult(g, err, ns == deny, tc.denial)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					current := tc.object.DeepCopyObject().(client.Object)
					g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(base), current)).To(Succeed())
					g.Expect(current.GetLabels()).NotTo(HaveKey("event-trigger"))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		}
		fmt.Fprintf(GinkgoWriter, "Dry-run Events: 20 CREATE/UPDATE admissions across five resource kinds and two namespace profiles took %s including setup and verification\n", time.Since(start))
		Consistently(func(g Gomega) {
			for _, ns := range []*corev1.Namespace{audit, deny} {
				list := &eventsv1.EventList{}
				g.Expect(k8sClient.List(ctx, list, client.InNamespace(ns.Name))).To(Succeed())
				for _, event := range list.Items {
					if event.ReportingController == events.ReportingController {
						g.Expect(event.Reason).NotTo(Equal(events.ReasonNamespaceRuleAudit))
						for _, tc := range cases {
							g.Expect(event.Reason).NotTo(Equal(tc.reason))
						}
					}
				}
			}
		}, 3*defaultPollInterval, defaultPollInterval).Should(Succeed())

		By("retaining audit and denial Events for normal CREATE and UPDATE")
		for _, ns := range []*corev1.Namespace{audit, deny} {
			for _, tc := range cases {
				obj := candidate(tc.object, ns.Name, "live-create-"+tc.kind, true)
				checkResult(Default, ownerA.Create(ctx, obj), ns == deny, tc.denial)
				current := tc.object.DeepCopyObject().(client.Object)
				err := ownerA.Get(ctx, client.ObjectKeyFromObject(obj), current)
				if ns == deny {
					Expect(apierrors.IsNotFound(err)).To(BeTrue())
				} else {
					Expect(err).NotTo(HaveOccurred())
					Expect(current.GetLabels()).To(HaveKeyWithValue("event-trigger", "yes"))
				}
				updateKey := client.ObjectKey{Namespace: ns.Name, Name: "update-" + tc.kind}
				Eventually(func(g Gomega) {
					latest := tc.object.DeepCopyObject().(client.Object)
					g.Expect(ownerA.Get(ctx, updateKey, latest)).To(Succeed())
					latest.GetLabels()["event-trigger"] = "yes"
					err := ownerA.Update(ctx, latest)
					if apierrors.IsConflict(err) {
						g.Expect(err).NotTo(HaveOccurred())
						return
					}
					checkResult(g, err, ns == deny, tc.denial)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					stored := tc.object.DeepCopyObject().(client.Object)
					g.Expect(ownerA.Get(ctx, updateKey, stored)).To(Succeed())
					if ns == deny {
						g.Expect(stored.GetLabels()).NotTo(HaveKey("event-trigger"))
					} else {
						g.Expect(stored.GetLabels()).To(HaveKeyWithValue("event-trigger", "yes"))
					}
					list := &eventsv1.EventList{}
					g.Expect(k8sClient.List(ctx, list, client.InNamespace(ns.Name))).To(Succeed())
					wantReason := events.ReasonNamespaceRuleAudit
					if ns == deny {
						wantReason = tc.reason
					}
					var names []string
					for _, event := range list.Items {
						if event.Reason == wantReason && event.Labels[meta.NewTenantLabel] == a.Name {
							names = append(names, event.Regarding.Name)
						}
					}
					g.Expect(names).To(ContainElements(obj.GetName(), updateKey.Name))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
		}
		By("keeping non-selected namespaces and another tenant independent after dry-run denials")
		for _, sample := range []struct {
			ns    *corev1.Namespace
			actor client.Client
		}{{other, ownerA}, {isolated, ownerB}} {
			obj := candidate(cases[0].object, sample.ns.Name, "independent", true)
			Expect(sample.actor.Create(ctx, obj)).To(Succeed())
			Eventually(func(g Gomega) {
				stored := &networkingv1.NetworkPolicy{}
				g.Expect(sample.actor.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				g.Expect(stored.Labels).To(HaveKeyWithValue("event-trigger", "yes"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Eventually(func(g Gomega) {
			list := &eventsv1.EventList{}
			g.Expect(k8sClient.List(ctx, list, client.InNamespace(isolated.Name))).To(Succeed())
			var tenants []string
			for _, event := range list.Items {
				if event.Reason == events.ReasonNamespaceRuleAudit && event.Regarding.Name == "independent" {
					tenants = append(tenants, event.Labels[meta.NewTenantLabel])
				}
			}
			g.Expect(tenants).To(ConsistOf(b.Name, b.Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		cross := candidate(cases[0].object, isolated.Name, "cross-tenant", true)
		Expect(ownerA.Create(ctx, cross, client.DryRunAll)).To(MatchError(ContainSubstring("forbidden")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &networkingv1.NetworkPolicy{}))).To(BeTrue())

		By("applying changed namespace profiles to dry-run decisions")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current); err != nil {
				return err
			}
			current.Labels["event-profile"] = "deny"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitRules(other, rules.ActionTypeDeny)
		changed := candidate(cases[0].object, other.Name, "changed-profile", true)
		Expect(ownerA.Create(ctx, changed, client.DryRunAll)).To(MatchError(ContainSubstring("metadata label")))
		Expect(apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(changed), &networkingv1.NetworkPolicy{}))).To(BeTrue())
		Consistently(func(g Gomega) {
			list := &eventsv1.EventList{}
			g.Expect(k8sClient.List(ctx, list, client.InNamespace(other.Name))).To(Succeed())
			for _, event := range list.Items {
				g.Expect(event.Regarding.Name).NotTo(Equal("changed-profile"))
			}
		}, 3*defaultPollInterval, defaultPollInterval).Should(Succeed())
	})
})
