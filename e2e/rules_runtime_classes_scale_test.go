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
	eventsv1 "k8s.io/api/events/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/runtime/events"
)

// An audience-scoped class rule never matches the Pods a controller creates:
// they arrive under the controller's identity. The controller admission of the
// rule's audience is therefore its only enforcement point, and every update
// that can create Pods — a replica bump, a rollout, a scale subresource write —
// must be judged even when the template class is unchanged.
var _ = Describe("RuntimeClass template scale enforcement", Label("tenant", "rules", "workloads", "runtimeclass", "runtime-class-rules"), func() {
	It("enforces audience-scoped class rules on updates that create Pods", func() {
		ctx := context.Background()
		prefix := "e2e-rc-scale-" + rand.String(8)
		forbidden, allowed := prefix+"-forbidden", prefix+"-allowed"

		for _, name := range []string{forbidden, allowed} {
			class := &nodev1.RuntimeClass{Name: name, Labels: map[string]string{"env": "e2e"}, Handler: "e2e-runtime-class"}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(DeleteInBackground, class)
		}

		// The rule is scoped to the tenant owners: an administrator stays free
		// to manage the workload, and the owners may not launch its Pods.
		a := &capsulev1beta2.Tenant{Name: prefix + "-a", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-a"}},
			Rules: []*rules.NamespaceRuleBodyTenant{{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}},
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action: rules.ActionTypeDeny,
					Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
						Targets: []rules.WorkloadValidationTarget{rules.ValidateDeployment},
						Classes: rules.WorkloadClassEnforcement{Runtime: []rules.WorkloadClassMatch{{Exact: []string{forbidden}}}},
					},
				},
			}}},
		}}
		b := &capsulev1beta2.Tenant{Name: prefix + "-b", Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
			Owners: rbac.OwnerListSpec{{Kind: "User", Name: prefix + "-b"}},
		}}

		for _, tnt := range []*capsulev1beta2.Tenant{a, b} {
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}

		ownerA := impersonationClient(a.Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Name, withDefaultGroups(nil))

		newNS := func(tnt *capsulev1beta2.Tenant, rulesWanted int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, ns.Name)
			waitWorkloadClassProfile(ctx, ns, func(g Gomega, bodies []*rules.NamespaceRuleBodyNamespace) {
				g.Expect(bodies).To(HaveLen(rulesWanted))
			})

			return ns
		}
		nsA := newNS(a, 1)
		nsB := newNS(b, 0)

		deployment := func(namespace, name, class string) *appsv1.Deployment {
			dep := MakeDeployment(namespace, name, 0, map[string]string{"env": "e2e"}, "")
			dep.Labels = map[string]string{"env": "e2e"}
			dep.Spec.Template.Spec.RuntimeClassName = &class

			return dep
		}
		assertReplicas := func(dep *appsv1.Deployment, replicas int32) {
			Eventually(func(g Gomega) {
				current := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dep), current)).To(Succeed())
				g.Expect(current.Spec.Replicas).To(HaveValue(Equal(replicas)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		scaleTo := func(actor client.Client, dep *appsv1.Deployment, replicas int32, denied ...string) {
			Eventually(func(g Gomega) {
				current := &appsv1.Deployment{}
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(dep), current)).To(Succeed())

				scale := &autoscalingv1.Scale{}
				g.Expect(actor.SubResource("scale").Get(ctx, current, scale)).To(Succeed())
				scale.Spec.Replicas = replicas

				err := actor.SubResource("scale").Update(ctx, current, client.WithSubResourceBody(scale))
				if len(denied) == 0 {
					g.Expect(err).To(Succeed())

					return
				}

				if err == nil {
					Fail("a forbidden scale subresource write succeeded")
				}

				for _, want := range denied {
					g.Expect(err).To(MatchError(ContainSubstring(want)))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		By("letting an administrator create a paused workload the owners may not launch")
		grandfathered := deployment(nsA.Name, "grandfathered", forbidden)
		Eventually(func() error { return k8sClient.Create(ctx, grandfathered) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting the owner's replica bump although the template class is unchanged")
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(grandfathered), current)).To(Succeed())
			current.Spec.Replicas = new(int32(2))

			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("a scale-up of a forbidden template class succeeded")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("spec.template"), ContainSubstring("is denied by namespace rule"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting the owner's rollout that keeps the template class")
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(grandfathered), current)).To(Succeed())
			current.Spec.Template.Labels["revision"] = "two"

			err := ownerA.Update(ctx, current)
			if err == nil {
				Fail("a rollout with a forbidden template class succeeded")
			}

			g.Expect(err).To(MatchError(And(ContainSubstring("spec.template"), ContainSubstring("is denied by namespace rule"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("rejecting the owner's scale subresource write kubectl and autoscalers use")
		scaleTo(ownerA, grandfathered, 2, "spec.template", "is denied by namespace rule")
		assertReplicas(grandfathered, 0)

		By("recording the denial on the Deployment")
		Eventually(func(g Gomega) {
			list := &eventsv1.EventList{}
			g.Expect(k8sClient.List(ctx, list, client.InNamespace(nsA.Name))).To(Succeed())

			var reasons []string

			for _, event := range list.Items {
				if event.ReportingController == events.ReportingController && event.Regarding.Kind == "Deployment" && event.Regarding.Name == grandfathered.Name {
					g.Expect(event.Labels[meta.NewTenantLabel]).To(Equal(a.Name))

					reasons = append(reasons, event.Reason)
				}
			}

			g.Expect(reasons).To(ContainElement(events.ReasonForbiddenRuntimeClass))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("keeping the administrator outside the rule's audience")
		scaleTo(k8sClient, grandfathered, 1)
		assertReplicas(grandfathered, 1)

		By("letting the owner scale the forbidden workload down: that creates no Pods")
		scaleTo(ownerA, grandfathered, 0)
		assertReplicas(grandfathered, 0)

		By("letting the owner run and scale a workload with an admitted class")
		own := deployment(nsA.Name, "owned", allowed)
		Eventually(func() error { return ownerA.Create(ctx, own) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &appsv1.Deployment{}
			g.Expect(ownerA.Get(ctx, client.ObjectKeyFromObject(own), current)).To(Succeed())
			current.Spec.Replicas = new(int32(1))
			g.Expect(ownerA.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		scaleTo(ownerA, own, 2)
		assertReplicas(own, 2)

		By("isolating another tenant from these rules")
		unrelated := deployment(nsB.Name, "unrelated", forbidden)
		Eventually(func() error { return ownerB.Create(ctx, unrelated) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		scaleTo(ownerB, unrelated, 1)
		assertReplicas(unrelated, 1)
	})
})
