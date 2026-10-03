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

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("image pull secrets mutation", Label("tenant", "rules", "workloads", "image-pull-secrets-mutation"), func() {
	It("merges, replaces and clears namespace profiles without changing other tenants or existing Pods", func() {
		ctx := context.Background()
		prefix := "e2e-pull-secret-" + rand.String(8)
		refs := func(names ...string) []corev1.LocalObjectReference {
			result := make([]corev1.LocalObjectReference, len(names))
			for i, name := range names {
				result[i].Name = name
			}
			return result
		}
		mutation := func(action rules.MutationAction, names ...string) rules.NamespaceRuleMutation {
			return rules.NamespaceRuleMutation{Action: action, Workloads: rules.WorkloadMutation{Targets: []rules.WorkloadValidationTarget{rules.ValidatePod}, Registries: rules.WorkloadRegistryMutation{ImagePullSecrets: refs(names...)}}}
		}
		var tenants []*capsule.Tenant
		var owners []client.Client
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			composed := mutation(rules.MutationActionMerge, name+"-pull", "shared-pull")
			composed.Conditions = []rules.AdmissionCondition{{Expression: "object.spec.imagePullSecrets.exists(s, s.name == '" + name + "-pull')"}}
			tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsule.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}}}}
			for i, profile := range []string{"merge", "replace", "clear"} {
				entries := []rules.NamespaceRuleMutation{mutation(rules.MutationActionMerge, name+"-pull"), composed}
				if i == 1 {
					entries = []rules.NamespaceRuleMutation{mutation(rules.MutationActionReplace, name+"-pull")}
				}
				if i == 2 {
					entries = []rules.NamespaceRuleMutation{mutation(rules.MutationActionReplace)}
				}
				tnt.Spec.Rules = append(tnt.Spec.Rules, &rules.NamespaceRuleBodyTenant{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"pull-secrets": profile}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: entries}})
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		waitProfile := func(ns *corev1.Namespace, tenantIndex, ruleIndex int) {
			Eventually(func(g Gomega) {
				current := &capsule.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[tenantIndex]), current)).To(Succeed())
				status := &capsule.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				if ruleIndex < 0 {
					g.Expect(status.Status.Rules).To(BeEmpty())
				} else {
					g.Expect(status.Status.Rules).To(HaveLen(1))
					g.Expect(status.Status.Rules[0].Mutate).To(Equal(current.Spec.Rules[ruleIndex].Mutate))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(index int, profile string, rule int) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[index].Name, "pull-secrets": profile, "pod-security.kubernetes.io/enforce": "privileged"})
			NamespaceCreation(ns, tenants[index].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[index], ns).Should(Succeed())
			waitProfile(ns, index, rule)
			for _, name := range []string{"user-pull", "shared-pull", tenants[index].Name + "-pull"} {
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Type: corev1.SecretTypeDockerConfigJson, Data: map[string][]byte{corev1.DockerConfigJsonKey: []byte(`{"auths":{}}`)}}
				Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			}
			return ns
		}
		selected, replaced, cleared, plain, isolated := newNS(0, "merge", 0), newNS(0, "replace", 1), newNS(0, "clear", 2), newNS(0, "plain", -1), newNS(1, "merge", 0)
		newPod := func(ns *corev1.Namespace, name string) *corev1.Pod {
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: corev1.PodSpec{SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/pull-secrets-test"}}, ImagePullSecrets: refs("user-pull"), Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}}}}
		}
		verify := func(pod *corev1.Pod, names ...string) {
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
				if len(names) == 0 {
					g.Expect(stored.Spec.ImagePullSecrets).To(BeEmpty())
				} else {
					g.Expect(stored.Spec.ImagePullSecrets).To(Equal(refs(names...)))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("applying each namespace's ordered profile")
		pod := newPod(selected, "merged")
		Expect(owners[0].Create(ctx, pod)).To(Succeed())
		verify(pod, "user-pull", tenants[0].Name+"-pull", "shared-pull")
		for _, tc := range []struct {
			ns       *corev1.Namespace
			name     string
			expected []string
		}{
			{replaced, "replaced", []string{tenants[0].Name + "-pull"}}, {cleared, "cleared", nil}, {plain, "unchanged", []string{"user-pull"}},
		} {
			current := newPod(tc.ns, tc.name)
			Expect(owners[0].Create(ctx, current)).To(Succeed())
			verify(current, tc.expected...)
		}
		bPod := newPod(isolated, "isolated")
		Expect(owners[1].Create(ctx, bPod)).To(Succeed())
		verify(bPod, "user-pull", tenants[1].Name+"-pull", "shared-pull")
		By("removing incoming duplicates and overlaps between ordered rules in both tenants")
		for i, ns := range []*corev1.Namespace{selected, isolated} {
			duplicates := newPod(ns, "deduplicated")
			duplicates.Spec.ImagePullSecrets = refs("user-pull", "shared-pull", "user-pull", "shared-pull")
			Expect(owners[i].Create(ctx, duplicates)).To(Succeed())
			verify(duplicates, "user-pull", "shared-pull", tenants[i].Name+"-pull")
		}
		cross := newPod(isolated, "cross-tenant")
		Expect(owners[0].Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource \"pods\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.Pod{}))).To(BeTrue())
		Expect(owners[0].Get(ctx, client.ObjectKey{Namespace: isolated.Name, Name: tenants[1].Name + "-pull"}, &corev1.Secret{})).To(MatchError(ContainSubstring("cannot get resource \"secrets\"")))

		By("rejecting a reference to another namespace without storing it")
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate[0].Workloads.Registries.ImagePullSecrets = refs(isolated.Name + "/registry")
			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("invalid cross-namespace secret reference was stored")
			}
			g.Expect(err).To(MatchError(And(ContainSubstring("imagePullSecrets"), ContainSubstring("invalid secret name"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedTenant := &capsule.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules).To(Equal(tenants[0].Spec.Rules))

		By("rejecting duplicate configured names for both actions without storing them")
		for _, action := range []rules.MutationAction{rules.MutationActionMerge, rules.MutationActionReplace} {
			Eventually(func(g Gomega) {
				current := &capsule.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
				current.Spec.Rules[0].Mutate[0] = mutation(action, "shared-pull", "shared-pull")
				err := k8sClient.Update(ctx, current)
				if err == nil {
					Fail("duplicate secret references were stored")
				}
				g.Expect(err).To(MatchError(And(ContainSubstring("imagePullSecrets"), MatchRegexp("(?i)duplicate"))))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
			Expect(storedTenant.Spec.Rules).To(Equal(tenants[0].Spec.Rules))
		}

		By("applying policy and label changes only to future Pods")
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Mutate = []rules.NamespaceRuleMutation{mutation(rules.MutationActionReplace)}
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(selected, 0, 0)
		after := newPod(selected, "after-rule-change")
		Expect(owners[0].Create(ctx, after)).To(Succeed())
		verify(after)
		Eventually(func(g Gomega) {
			current := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plain), current)).To(Succeed())
			current.Labels["pull-secrets"] = "replace"
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(plain, 0, 1)
		afterLabel := newPod(plain, "after-label-change")
		Expect(owners[0].Create(ctx, afterLabel)).To(Succeed())
		verify(afterLabel, tenants[0].Name+"-pull")
		Eventually(func(g Gomega) {
			current := &corev1.Pod{}
			g.Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(pod), current)).To(Succeed())
			current.Labels["example.com/updated"] = "true"
			g.Expect(owners[0].Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		verify(pod, "user-pull", tenants[0].Name+"-pull", "shared-pull")
		verify(bPod, "user-pull", tenants[1].Name+"-pull", "shared-pull")
	})
})
