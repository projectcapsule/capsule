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
)

var _ = Describe("read-only root filesystem mutation", Label("tenant", "rules", "workloads", "read-only-root-filesystem"), func() {
	It("selects container groups within namespace profiles and patches only new ephemeral containers", func() {
		ctx := context.Background()
		prefix := "e2e-rootfs-" + rand.String(8)
		var tenants []*capsule.Tenant
		var owners []client.Client
		mutation := func(value bool, target rules.WorkloadValidationTarget) rules.NamespaceRuleMutation {
			return rules.NamespaceRuleMutation{Action: rules.MutationActionMerge, Workloads: rules.WorkloadMutation{Targets: []rules.WorkloadValidationTarget{target}, Security: rules.WorkloadSecurityMutation{ReadOnlyRootFilesystem: new(value)}}}
		}
		for i, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			last := mutation(i == 0, rules.ValidatePod)
			last.Action = rules.MutationActionReplace
			last.Conditions = []rules.AdmissionCondition{{Expression: "true"}}
			skipped := mutation(false, rules.ValidatePod)
			skipped.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
			tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsule.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}}, Rules: []*rules.NamespaceRuleBodyTenant{
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"rootfs": "all"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation(false, rules.ValidatePod), last, skipped}}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"rootfs": "init"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{mutation(true, rules.ValidateInitContainers)}}},
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
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(index int, profile string, rule int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[index].Name, "rootfs": profile, "pod-security.kubernetes.io/enforce": "privileged"})
			NamespaceCreation(ns, tenants[index].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[index], ns).Should(Succeed())
			waitProfile(ns, index, rule)
			return ns
		}
		selected, initOnly, plain, isolated := newNS(0, "all", 0), newNS(0, "init", 1), newNS(0, "other", -1), newNS(1, "all", 0)
		newPod := func(ns *corev1.Namespace, name string, initial bool) *corev1.Pod {
			container := func(name string) corev1.Container {
				security := restrictedContainerSecurityContext()
				security.ReadOnlyRootFilesystem = new(initial)
				return corev1.Container{Name: name, Image: "registry.k8s.io/pause:3.10", SecurityContext: security}
			}
			sidecar := container("sidecar")
			sidecar.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: corev1.PodSpec{SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/rootfs-test"}}, Containers: []corev1.Container{container("app")}, InitContainers: []corev1.Container{container("init"), sidecar}}}
		}
		verify := func(pod *corev1.Pod, regular, init bool) {
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
				g.Expect(stored.Spec.Containers[0].SecurityContext.ReadOnlyRootFilesystem).To(Equal(new(regular)))
				for _, container := range stored.Spec.InitContainers {
					g.Expect(container.SecurityContext.ReadOnlyRootFilesystem).To(Equal(new(init)))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("composing ordered mutations while respecting namespace and tenant boundaries")
		pod := newPod(selected, "all-containers", false)
		Expect(owners[0].Create(ctx, pod)).To(Succeed())
		verify(pod, true, true)
		initPod := newPod(initOnly, "init-only", false)
		Expect(owners[0].Create(ctx, initPod)).To(Succeed())
		verify(initPod, false, true)
		plainPod := newPod(plain, "unchanged", false)
		Expect(owners[0].Create(ctx, plainPod)).To(Succeed())
		verify(plainPod, false, false)
		bPod := newPod(isolated, "writable", true)
		Expect(owners[1].Create(ctx, bPod)).To(Succeed())
		verify(bPod, false, false)
		cross := newPod(isolated, "cross-tenant", false)
		Expect(owners[0].Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource \"pods\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.Pod{}))).To(BeTrue())

		By("rejecting unsupported targets without storing an invalid policy")
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[0].Workloads.Targets = []rules.WorkloadValidationTarget{rules.ValidateDeployment}
			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("unsupported mutation target was stored")
			}
			g.Expect(err).To(MatchError(And(ContainSubstring("targets"), ContainSubstring("deployment"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedTenant := &capsule.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules).To(Equal(tenants[0].Spec.Rules))

		By("patching only newly added ephemeral containers, including after policy changes")
		DeferCleanup(GrantEphemeralContainersUpdate(selected.Name, tenants[0].Name))
		cs := ownerClient(tenants[0].Spec.Owners[0].UserSpec)
		addDebug := func(name string, expected ...bool) {
			Eventually(func(g Gomega) {
				current, err := cs.CoreV1().Pods(selected.Name).Get(ctx, pod.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				if !slices.ContainsFunc(current.Spec.EphemeralContainers, func(c corev1.EphemeralContainer) bool { return c.Name == name }) {
					security := restrictedContainerSecurityContext()
					security.ReadOnlyRootFilesystem = nil
					current.Spec.EphemeralContainers = append(current.Spec.EphemeralContainers, corev1.EphemeralContainer{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: name, Image: "registry.k8s.io/pause:3.10", SecurityContext: security}})
				}
				_, err = cs.CoreV1().Pods(selected.Name).UpdateEphemeralContainers(ctx, current.Name, current, metav1.UpdateOptions{})
				g.Expect(err).NotTo(HaveOccurred())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				stored, err := cs.CoreV1().Pods(selected.Name).Get(ctx, pod.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(stored.Spec.EphemeralContainers).To(HaveLen(len(expected)))
				for i, value := range expected {
					g.Expect(stored.Spec.EphemeralContainers[i].SecurityContext.ReadOnlyRootFilesystem).To(Equal(new(value)))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			verify(pod, true, true)
		}
		addDebug("debug-first", true)
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[1].Workloads.Security.ReadOnlyRootFilesystem = new(false)
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(selected, 0, 0)
		addDebug("debug-second", true, false)
		verify(bPod, false, false)

		By("applying label changes to future Pods without rewriting existing Pods")
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plain), current)).To(Succeed())
			current.Labels["rootfs"] = "init"
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(plain, 0, 1)
		after := newPod(plain, "new-profile", false)
		Expect(owners[0].Create(ctx, after)).To(Succeed())
		verify(after, false, true)
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(plainPod), stored)).To(Succeed())
			stored.Labels["example.com/updated"] = "true"
			g.Expect(owners[0].Update(ctx, stored)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		verify(plainPod, false, false)

	})
})
