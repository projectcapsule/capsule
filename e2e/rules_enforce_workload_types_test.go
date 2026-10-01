// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
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

var _ = Describe("workload type namespace profiles", Label("tenant", "rules", "workloads", "workload-types"), func() {
	It("enforces native kinds with conditions, ordering, audiences and isolated namespace profiles", func() {
		ctx := context.Background()
		prefix := "e2e-types-" + rand.String(8)
		policy := func(profile string, action rules.ActionType, types ...rules.WorkloadValidationTarget) *rules.NamespaceRuleBodyTenant {
			return &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"workload-profile": profile}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: action, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: types}}}}
		}
		a := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}}, Rules: []*rules.NamespaceRuleBodyTenant{
			policy("restricted", rules.ActionTypeDeny, rules.ValidateDaemonSet),
			policy("allowlist", rules.ActionTypeAllow, rules.ValidateDeployment),
			policy("audit", rules.ActionTypeAudit, rules.ValidateDaemonSet),
			policy("conditional", rules.ActionTypeDeny, rules.ValidateDaemonSet),
			policy("restricted", rules.ActionTypeDeny, rules.ValidateDeployment),
			{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeDeny, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Schedulers: []apiruntime.ExpressionMatch{{Exact: []string{"forbidden-scheduler"}}}}}}},
		}}}
		a.Spec.Rules[3].Enforce.Conditions = []rules.AdmissionCondition{{Name: "blocked", Expression: `has(object.metadata.labels) && 'blocked' in object.metadata.labels && object.metadata.labels['blocked'] == 'true'`}}
		a.Spec.Rules[4].Audience = []rules.Audience{{Kind: rules.AudienceKindUser, Name: "not-the-workload-owner"}}
		b := &capsulev1beta2.Tenant{ObjectMeta: metav1.ObjectMeta{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}}, Rules: []*rules.NamespaceRuleBodyTenant{policy("restricted", rules.ActionTypeDeny, rules.ValidateJob)}}}
		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		ownerA := impersonationClient(a.Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Name, withDefaultGroups(nil))
		waitTypes := func(ns *corev1.Namespace, want ...rules.WorkloadValidationTarget) {
			Eventually(func(g Gomega) {
				rs := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, rs)).To(Succeed())
				g.Expect(rs.Status.ObservedGeneration).To(Equal(rs.Generation))
				ready := rs.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				var got []rules.WorkloadValidationTarget
				for _, body := range rs.Status.Rules {
					if body.Enforce != nil {
						got = append(got, body.Enforce.Workloads.Targets...)
					}
				}
				g.Expect(got).To(Equal(want))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(tnt *capsulev1beta2.Tenant, profile string, want ...rules.WorkloadValidationTarget) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "workload-profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			waitTypes(ns, want...)
			return ns
		}
		restricted := newNS(a, "restricted", rules.ValidateDaemonSet, rules.ValidateDeployment)
		other := newNS(a, "other")
		allowlist := newNS(a, "allowlist", rules.ValidateDeployment)
		audited := newNS(a, "audit", rules.ValidateDaemonSet)
		conditional := newNS(a, "conditional", rules.ValidateDaemonSet)
		isolated := newNS(b, "restricted", rules.ValidateJob)
		daemon := func(ns *corev1.Namespace, name string) *appsv1.DaemonSet {
			dep := MakeDeployment(ns.Name, name, 0, map[string]string{"env": "e2e"}, "")
			dep.Spec.Template.Spec.NodeSelector = map[string]string{"workload-types.example.com/node": prefix}
			return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: appsv1.DaemonSetSpec{Selector: dep.Spec.Selector, Template: dep.Spec.Template}}
		}
		create := func(actor client.Client, obj client.Object) {
			Expect(actor.Create(ctx, obj)).To(Succeed())
			Eventually(func(g Gomega) {
				stored := obj.DeepCopyObject().(client.Object)
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				g.Expect(stored.GetUID()).To(Equal(obj.GetUID()))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		denyCreate := func(actor client.Client, obj client.Object, kind string) {
			err := actor.Create(ctx, obj)
			Expect(err).To(MatchError(And(ContainSubstring("workload type"), ContainSubstring(kind))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj.DeepCopyObject().(client.Object)))).To(BeTrue())
		}
		By("rejecting DaemonSets while preserving other types, profiles and tenants")
		denyCreate(ownerA, daemon(restricted, "denied"), "DaemonSet")
		dep := MakeDeployment(restricted.Name, "allowed-deployment", 0, map[string]string{"env": "e2e"}, "")
		dep.Labels = map[string]string{"env": "e2e"}
		create(ownerA, dep)
		existing := daemon(other, "existing")
		create(ownerA, existing)
		create(ownerB, daemon(isolated, "tenant-b"))
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: allowlist.Name, Labels: map[string]string{"env": "e2e"}}}
		create(ownerA, cm)

		By("allowing a controller without implicitly allowing its children")
		parent := MakeDeployment(allowlist.Name, "parent", 0, map[string]string{"env": "e2e"}, "")
		parent.Labels = map[string]string{"env": "e2e"}
		parent.Spec.Template.Spec.SchedulingGates = []corev1.PodSchedulingGate{{Name: "example.com/e2e-types"}}
		create(ownerA, parent)
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "child", Namespace: allowlist.Name, Labels: map[string]string{"env": "e2e"}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(parent, appsv1.SchemeGroupVersion.WithKind("Deployment"))}}, Spec: appsv1.ReplicaSetSpec{Replicas: new(int32(0)), Selector: parent.Spec.Selector, Template: parent.Spec.Template}}
		denyCreate(ownerA, rs, "ReplicaSet")

		By("auditing instead of rejecting the selected type")
		create(ownerA, daemon(audited, "audited"))
		Eventually(func(g Gomega) {
			list := &eventsv1.EventList{}
			g.Expect(k8sClient.List(ctx, list, client.InNamespace(audited.Name))).To(Succeed())
			found := false
			for _, event := range list.Items {
				if event.Reason == events.ReasonNamespaceRuleAudit && event.Regarding.Kind == "DaemonSet" && event.Regarding.Name == "audited" {
					found = true
					g.Expect(event.Note).To(ContainSubstring("workload type"))
				}
			}
			g.Expect(found).To(BeTrue())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("reevaluating conditions on updates even when the workload kind is unchanged")
		conditionalDS := daemon(conditional, "conditional")
		create(ownerA, conditionalDS)
		blocked := daemon(conditional, "blocked")
		blocked.Labels["blocked"] = "true"
		denyCreate(ownerA, blocked, "DaemonSet")
		Eventually(func(g Gomega) {
			current := &appsv1.DaemonSet{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(conditionalDS), current)).To(Succeed())
			current.Labels["blocked"] = "true"
			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("conditional DaemonSet update was accepted")
			}
			g.Expect(err).To(MatchError(ContainSubstring("workload type")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		stored := &appsv1.DaemonSet{}
		Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(conditionalDS), stored)).To(Succeed())
		Expect(stored.Labels).NotTo(HaveKey("blocked"))

		By("applying namespace-label changes and retaining deletion")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(other), current); err != nil {
				return err
			}
			current.Labels["workload-profile"] = "restricted"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitTypes(other, rules.ValidateDaemonSet, rules.ValidateDeployment)
		Eventually(func(g Gomega) {
			current := &appsv1.DaemonSet{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(existing), current)).To(Succeed())
			current.Labels["update"] = "true"
			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("restricted DaemonSet update was accepted")
			}
			g.Expect(err).To(MatchError(ContainSubstring("workload type")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(existing), stored)).To(Succeed())
		Expect(stored.Labels).NotTo(HaveKey("update"))
		Expect(ownerA.Delete(ctx, existing)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(ownerA.Get(ctx, client.ObjectKeyFromObject(existing), &appsv1.DaemonSet{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		denyCreate(ownerA, daemon(other, existing.Name), "DaemonSet")

		By("applying later allow rules and permitting controller-created ReplicaSets and Pods explicitly")
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Spec.Rules[1].Enforce.Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment, rules.ValidateReplicaSet, rules.ValidatePod}
			if len(current.Spec.Rules) == 6 {
				current.Spec.Rules = append(current.Spec.Rules, policy("restricted", rules.ActionTypeAllow, rules.ValidateDaemonSet, rules.ValidateDeployment, rules.ValidateReplicaSet, rules.ValidatePod))
			}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitTypes(allowlist, rules.ValidateDeployment, rules.ValidateReplicaSet, rules.ValidatePod)
		waitTypes(restricted, rules.ValidateDaemonSet, rules.ValidateDeployment, rules.ValidateDaemonSet, rules.ValidateDeployment, rules.ValidateReplicaSet, rules.ValidatePod)
		create(ownerA, daemon(restricted, "exception"))
		Eventually(func() error {
			current := &appsv1.Deployment{}
			if err := ownerA.Get(ctx, client.ObjectKeyFromObject(parent), current); err != nil {
				return err
			}
			current.Spec.Replicas = new(int32(1))
			return ownerA.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(parent), current)).To(Succeed())
			g.Expect(current.Status.ObservedGeneration).To(Equal(current.Generation))
			pods := &corev1.PodList{}
			g.Expect(k8sClient.List(ctx, pods, client.InNamespace(allowlist.Name), client.MatchingLabels{"app": parent.Name})).To(Succeed())
			g.Expect(pods.Items).To(HaveLen(1))
			g.Expect(pods.Items[0].OwnerReferences).To(ContainElement(HaveField("Kind", "ReplicaSet")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("keeping Pod property enforcement independent of type allows")
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "forbidden-scheduler", Namespace: allowlist.Name, Labels: map[string]string{"env": "e2e"}}, Spec: parent.Spec.Template.Spec}
		pod.Spec.SchedulerName = "forbidden-scheduler"
		Expect(ownerA.Create(ctx, pod)).To(MatchError(ContainSubstring("spec.schedulerName")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		create(ownerB, daemon(isolated, "still-isolated"))
		By("preserving cross-tenant RBAC")
		cross := daemon(isolated, "cross-tenant")
		err := ownerA.Create(ctx, cross)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "daemonsets"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &appsv1.DaemonSet{}))).To(BeTrue())
	})
})
