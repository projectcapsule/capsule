// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("image pull policy mutation", Label("tenant", "rules", "workloads", "image-pull-policy-mutation"), func() {
	It("selects container groups within namespace profiles and patches only new ephemeral containers", func() {
		const image = "registry.k8s.io/pause:3.10"
		ctx := context.Background()
		prefix := "e2e-pull-" + rand.String(8)
		var tenants []*capsule.Tenant
		var owners []client.Client
		mutation := func(value corev1.PullPolicy, target rules.WorkloadValidationTarget) rules.NamespaceRuleMutation {
			return rules.NamespaceRuleMutation{Action: rules.MutationActionMerge, Workloads: rules.WorkloadMutation{Targets: []rules.WorkloadValidationTarget{target}, Registries: rules.WorkloadRegistryMutation{ImagePullPolicy: value}}}
		}
		for i, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			policy := corev1.PullAlways
			if i != 0 {
				policy = corev1.PullNever
			}
			last := mutation(policy, rules.ValidatePod)
			last.Action = rules.MutationActionReplace
			last.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.containers[0].imagePullPolicy == 'IfNotPresent' || request.subResource == 'ephemeralcontainers'"}}
			skipped := mutation(corev1.PullIfNotPresent, rules.ValidatePod)
			skipped.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
			tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsule.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}}, Rules: []*rules.NamespaceRuleBodyTenant{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pull-profile": "all"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation(corev1.PullIfNotPresent, rules.ValidatePod), last, skipped}}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pull-profile": "init"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation(corev1.PullAlways, rules.ValidateInitContainers)}}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pull-profile": "conflict"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Mutate: []rules.NamespaceRuleMutation{mutation(corev1.PullNever, rules.ValidatePod)},
					Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{
						Registries: []rules.OCIRegistry{{ExpressionMatch: apiruntime.ExpressionMatch{Exact: []string{image}}, Policy: []corev1.PullPolicy{corev1.PullAlways}}},
					}},
				}},
			}}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		waitProfile := func(ns *corev1.Namespace, index, rule int) {
			Eventually(func(g Gomega) {
				current := &capsule.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[index]), current)).To(Succeed())
				status := &capsule.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				if rule < 0 {
					g.Expect(status.Status.Rules).To(BeEmpty())
				} else {
					g.Expect(status.Status.Rules).To(HaveLen(1))
					g.Expect(status.Status.Rules[0].Mutate).To(Equal(current.Spec.Rules[rule].Mutate))
					g.Expect(status.Status.Rules[0].Enforce).To(Equal(current.Spec.Rules[rule].Enforce))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(index int, profile string, rule int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[index].Name, "pull-profile": profile, "pod-security.kubernetes.io/enforce": "privileged"})
			NamespaceCreation(ns, tenants[index].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[index], ns).Should(Succeed())
			waitProfile(ns, index, rule)
			return ns
		}
		selected, initOnly, plain, isolated := newNS(0, "all", 0), newNS(0, "init", 1), newNS(0, "other", -1), newNS(1, "all", 0)
		newPod := func(ns *corev1.Namespace, name string, initial corev1.PullPolicy) *corev1.Pod {
			container := func(name string) corev1.Container {
				security := restrictedContainerSecurityContext()
				return corev1.Container{Name: name, Image: image, ImagePullPolicy: initial, SecurityContext: security}
			}
			sidecar := container("sidecar")
			sidecar.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: corev1.PodSpec{SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/pull-policy-test"}}, Containers: []corev1.Container{container("app")}, InitContainers: []corev1.Container{container("init"), sidecar}}}
		}
		verify := func(pod *corev1.Pod, regular, init corev1.PullPolicy) {
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
				g.Expect(stored.Spec.Containers[0].ImagePullPolicy).To(Equal(regular))
				for _, container := range stored.Spec.InitContainers {
					g.Expect(container.ImagePullPolicy).To(Equal(init))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("composing ordered mutations while respecting namespace and tenant boundaries")
		defaulted := newPod(selected, "defaulted-policy", "")
		Expect(owners[0].Create(ctx, defaulted)).To(Succeed())
		verify(defaulted, corev1.PullAlways, corev1.PullAlways)
		pod := newPod(selected, "all-containers", corev1.PullNever)
		Expect(owners[0].Create(ctx, pod)).To(Succeed())
		verify(pod, corev1.PullAlways, corev1.PullAlways)
		initPod := newPod(initOnly, "init-only", corev1.PullNever)
		Expect(owners[0].Create(ctx, initPod)).To(Succeed())
		verify(initPod, corev1.PullNever, corev1.PullAlways)
		plainPod := newPod(plain, "unchanged", corev1.PullNever)
		Expect(owners[0].Create(ctx, plainPod)).To(Succeed())
		verify(plainPod, corev1.PullNever, corev1.PullNever)
		bPod := newPod(isolated, "tenant-b-policy", corev1.PullAlways)
		Expect(owners[1].Create(ctx, bPod)).To(Succeed())
		verify(bPod, corev1.PullNever, corev1.PullNever)
		cross := newPod(isolated, "cross-tenant", corev1.PullAlways)
		Expect(owners[0].Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource \"pods\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.Pod{}))).To(BeTrue())

		By("enforcing image pull policy restrictions after mutation")
		conflictNS := newNS(0, "conflict", 2)
		conflict := newPod(conflictNS, "conflicting-policy", corev1.PullAlways)
		Expect(owners[0].Create(ctx, conflict)).To(MatchError(And(ContainSubstring("image pull policy"), ContainSubstring("Never"), ContainSubstring("not allowed"))))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(conflict), &corev1.Pod{}))).To(BeTrue())

		By("rejecting malformed pull policies without storing them")
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[0].Workloads.Registries.ImagePullPolicy = "Sometimes"
			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("invalid image pull policy was stored")
			}
			g.Expect(err).To(MatchError(And(ContainSubstring("imagePullPolicy"), ContainSubstring("Sometimes"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedTenant := &capsule.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules).To(Equal(tenants[0].Spec.Rules))

		By("admitting the same image once mutation satisfies its enforced pull policy")
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[2].Mutate[0].Workloads.Registries.ImagePullPolicy = corev1.PullAlways
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(conflictNS, 0, 2)
		allowed := newPod(conflictNS, "compatible-policy", corev1.PullNever)
		Expect(owners[0].Create(ctx, allowed)).To(Succeed())
		verify(allowed, corev1.PullAlways, corev1.PullAlways)

		By("patching only newly added ephemeral containers, including after policy changes")
		DeferCleanup(GrantEphemeralContainersUpdate(selected.Name, tenants[0].Name))
		cs := ownerClient(tenants[0].Spec.Owners[0].UserSpec)
		addDebug := func(name string, expected ...corev1.PullPolicy) {
			Eventually(func(g Gomega) {
				current, err := cs.CoreV1().Pods(selected.Name).Get(ctx, pod.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				if !slices.ContainsFunc(current.Spec.EphemeralContainers, func(c corev1.EphemeralContainer) bool { return c.Name == name }) {
					security := restrictedContainerSecurityContext()
					current.Spec.EphemeralContainers = append(current.Spec.EphemeralContainers, corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: name, Image: image, SecurityContext: security}})
				}
				_, err = cs.CoreV1().Pods(selected.Name).UpdateEphemeralContainers(ctx, current.Name, current, metav1.UpdateOptions{})
				g.Expect(err).NotTo(HaveOccurred())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				stored, err := cs.CoreV1().Pods(selected.Name).Get(ctx, pod.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(stored.Spec.EphemeralContainers).To(HaveLen(len(expected)))
				for i, value := range expected {
					g.Expect(stored.Spec.EphemeralContainers[i].ImagePullPolicy).To(Equal(value))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			verify(pod, corev1.PullAlways, corev1.PullAlways)
		}
		addDebug("debug-first", corev1.PullAlways)
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[1].Workloads.Registries.ImagePullPolicy = corev1.PullNever
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(selected, 0, 0)
		addDebug("debug-second", corev1.PullAlways, corev1.PullNever)
		verify(bPod, corev1.PullNever, corev1.PullNever)

		By("applying label changes to future Pods without rewriting existing Pods")
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plain), current)).To(Succeed())
			current.Labels["pull-profile"] = "init"
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(plain, 0, 1)
		after := newPod(plain, "new-profile", corev1.PullNever)
		Expect(owners[0].Create(ctx, after)).To(Succeed())
		verify(after, corev1.PullNever, corev1.PullAlways)
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(plainPod), stored)).To(Succeed())
			stored.Labels["example.com/updated"] = "true"
			g.Expect(owners[0].Update(ctx, stored)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		verify(plainPod, corev1.PullNever, corev1.PullNever)

	})
})
