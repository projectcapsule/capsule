// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
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

var _ = Describe("workload security profiles", Label("tenant", "rules", "workloads", "security-profiles"), func() {
	It("defaults and enforces profiles across namespace selection, templates and ephemeral containers", func() {
		ctx := context.Background()
		prefix := "e2e-profiles-" + rand.String(8)
		var tenants []*capsule.Tenant
		var owners []client.Client
		defaults := rules.WorkloadMutation{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeRuntimeDefault}}
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			matches := []rules.WorkloadSecurityProfileMatch{{Types: []rules.SecurityProfileType{rules.SecurityProfileRuntimeDefault, rules.SecurityProfileLocalhost}, LocalhostProfiles: []apiruntime.ExpressionMatch{{Exact: []string{name + ".json"}}}}}
			tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsule.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}}, Rules: []*rules.NamespaceRuleBodyTenant{
				{NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "security-profile", Operator: metav1.LabelSelectorOpIn, Values: []string{"default", "replace"}}}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
					Mutate:  []rules.NamespaceRuleMutation{{Workloads: defaults}},
					Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow, Workloads: rules.NamespaceRuleEnforceWorkloadsBody{Targets: []rules.WorkloadValidationTarget{rules.ValidateContainers, rules.ValidateInitContainers, rules.ValidateEphemeralContainers, rules.ValidateDeployment}, SeccompProfiles: matches, AppArmorProfiles: matches}},
				}},
				{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"security-profile": "replace"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Workloads: defaults}}}},
			}}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		By("rejecting malformed profile configuration without changing the Tenant")
		for _, field := range []string{"seccompProfiles", "appArmorProfile"} {
			Eventually(func(g Gomega) {
				current := &capsule.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
				if field == "seccompProfiles" {
					current.Spec.Rules[0].Enforce.Workloads.SeccompProfiles[0].LocalhostProfiles[0].Expression = "["
				} else {
					current.Spec.Rules[0].Mutate[0].Workloads.AppArmorProfile.LocalhostProfile = new("unexpected")
				}
				err := k8sClient.Update(ctx, current)
				if err == nil {
					Fail("malformed profile configuration was stored")
				}
				g.Expect(err).To(MatchError(ContainSubstring(field)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		storedTenant := &capsule.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules).To(Equal(tenants[0].Spec.Rules))
		By("accepting profile and expression limits and rejecting oversized values without replacing the stored policy")
		for _, group := range []string{"seccompProfiles", "appArmorProfiles"} {
			for _, limit := range []struct {
				field string
				count int
			}{
				{"matchers", 64}, {"types", 3}, {"localhostProfiles", 64},
				{"localhostProfiles[0].exact", 64}, {"localhostProfiles[0].exp", 4096},
			} {
				profiles := []rules.WorkloadSecurityProfileMatch{{Types: []rules.SecurityProfileType{rules.SecurityProfileLocalhost}, LocalhostProfiles: []apiruntime.ExpressionMatch{{Exact: []string{tenants[0].Name + ".json"}}}}}
				switch limit.field {
				case "matchers":
					profiles = slices.Repeat(profiles, limit.count)
				case "types":
					profiles[0].Types = []rules.SecurityProfileType{rules.SecurityProfileLocalhost, rules.SecurityProfileRuntimeDefault, rules.SecurityProfileUnconfined}
				case "localhostProfiles":
					profiles[0].LocalhostProfiles = slices.Repeat(profiles[0].LocalhostProfiles, limit.count)
				case "localhostProfiles[0].exact":
					profiles[0].LocalhostProfiles[0].Exact = slices.Repeat(profiles[0].LocalhostProfiles[0].Exact, limit.count)
				case "localhostProfiles[0].exp":
					profiles[0].LocalhostProfiles[0].Expression = strings.Repeat("a", limit.count)
				}
				set := func(tnt *capsule.Tenant) *[]rules.WorkloadSecurityProfileMatch {
					if group == "appArmorProfiles" {
						return &tnt.Spec.Rules[0].Enforce.Workloads.AppArmorProfiles
					}
					return &tnt.Spec.Rules[0].Enforce.Workloads.SeccompProfiles
				}
				Eventually(func(g Gomega) {
					current := &capsule.Tenant{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
					*set(current) = profiles
					g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Eventually(func(g Gomega) {
					current := &capsule.Tenant{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
					g.Expect(*set(current)).To(Equal(profiles))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				path := group
				if limit.field != "matchers" {
					path += "[0]." + limit.field
				}
				Eventually(func(g Gomega) {
					current := &capsule.Tenant{}
					g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
					matches := set(current)
					switch limit.field {
					case "matchers":
						*matches = append(*matches, (*matches)[0])
					case "types":
						(*matches)[0].Types = append((*matches)[0].Types, rules.SecurityProfileLocalhost)
					case "localhostProfiles":
						(*matches)[0].LocalhostProfiles = append((*matches)[0].LocalhostProfiles, (*matches)[0].LocalhostProfiles[0])
					case "localhostProfiles[0].exact":
						(*matches)[0].LocalhostProfiles[0].Exact = append((*matches)[0].LocalhostProfiles[0].Exact, "other.json")
					case "localhostProfiles[0].exp":
						(*matches)[0].LocalhostProfiles[0].Expression += "a"
					}
					err := k8sClient.Update(ctx, current)
					if err == nil {
						Fail("oversized profile or expression was stored")
					}
					g.Expect(err).To(MatchError(And(ContainSubstring(path), Or(
						ContainSubstring(fmt.Sprintf("at most %d", limit.count)),
						ContainSubstring(fmt.Sprintf("may not be more than %d", limit.count)),
					))))
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedTenant)).To(Succeed())
				Expect(*set(storedTenant)).To(Equal(profiles))
			}
		}
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Enforce.Workloads.SeccompProfiles = tenants[0].Spec.Rules[0].Enforce.Workloads.SeccompProfiles
			current.Spec.Rules[0].Enforce.Workloads.AppArmorProfiles = tenants[0].Spec.Rules[0].Enforce.Workloads.AppArmorProfiles
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[1]), storedTenant)).To(Succeed())
		Expect(storedTenant.Spec.Rules).To(Equal(tenants[1].Spec.Rules))
		waitProfile := func(ns *corev1.Namespace, count int, disabled bool) {
			Eventually(func(g Gomega) {
				status := &capsule.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				g.Expect(status.Status.Rules).To(HaveLen(count))
				if count > 0 {
					g.Expect(status.Status.Rules[0].Enforce.Workloads.SeccompProfiles).To(HaveLen(1))
					if disabled {
						g.Expect(status.Status.Rules[0].Enforce.Conditions).To(Equal([]rules.AdmissionCondition{{Expression: "false"}}))
					} else {
						g.Expect(status.Status.Rules[0].Enforce.Conditions).To(BeEmpty())
					}
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNS := func(index int, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[index].Name, "security-profile": profile, "pod-security.kubernetes.io/enforce": "privileged"})
			NamespaceCreation(ns, tenants[index].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[index], ns).Should(Succeed())
			count := 1
			if profile == "other" {
				count = 0
			}
			if profile == "replace" {
				count = 2
			}
			waitProfile(ns, count, false)
			return ns
		}
		selected, replaced, other, isolated := newNS(0, "default"), newNS(0, "replace"), newNS(0, "other"), newNS(1, "default")
		newPod := func(ns *corev1.Namespace, name string) *corev1.Pod {
			security := restrictedContainerSecurityContext()
			security.SeccompProfile = nil
			security.AppArmorProfile = nil
			return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}}, Spec: corev1.PodSpec{SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/profile-test"}}, Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.10", SecurityContext: security}}}}
		}
		localContext := func(name string) *corev1.PodSecurityContext {
			return &corev1.PodSecurityContext{SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeLocalhost, LocalhostProfile: new(name)}, AppArmorProfile: &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: new(name)}}
		}
		verify := func(pod *corev1.Pod, kind string) {
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
				g.Expect(stored.Spec.SecurityContext).NotTo(BeNil())
				g.Expect(stored.Spec.SecurityContext.SeccompProfile).NotTo(BeNil())
				g.Expect(stored.Spec.SecurityContext.AppArmorProfile).NotTo(BeNil())
				g.Expect(string(stored.Spec.SecurityContext.SeccompProfile.Type)).To(Equal(kind))
				g.Expect(string(stored.Spec.SecurityContext.AppArmorProfile.Type)).To(Equal(kind))
				g.Expect(stored.Spec.Containers[0].SecurityContext.SeccompProfile).To(BeNil())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("persisting defaults and preserving explicit approved profiles")
		good := newPod(selected, "defaulted")
		Expect(owners[0].Create(ctx, good)).To(Succeed())
		verify(good, "RuntimeDefault")
		custom := newPod(selected, "custom")
		custom.Spec.SecurityContext = localContext(tenants[0].Name + ".json")
		Expect(owners[0].Create(ctx, custom)).To(Succeed())
		verify(custom, "Localhost")
		forced := newPod(replaced, "replaced")
		forced.Spec.SecurityContext = localContext("obsolete.json")
		Expect(owners[0].Create(ctx, forced)).To(Succeed())
		verify(forced, "RuntimeDefault")
		By("rejecting container overrides and preserving non-selected namespaces")
		for _, field := range []string{"seccomp", "apparmor"} {
			pod := newPod(selected, field+"-blocked")
			if field == "seccomp" {
				pod.Spec.Containers[0].SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
			} else {
				pod.Spec.Containers[0].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
			}
			Expect(owners[0].Create(ctx, pod)).To(MatchError(And(ContainSubstring("profile"), ContainSubstring("Unconfined"))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
			pod.Namespace = other.Name
			Expect(owners[0].Create(ctx, pod)).To(Succeed())
			stored := &corev1.Pod{}
			Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
			Expect(stored.Spec.SecurityContext == nil || stored.Spec.SecurityContext.SeccompProfile == nil).To(BeTrue())
		}
		By("keeping approved localhost profiles tenant-scoped")
		bPod := newPod(isolated, "b-custom")
		bPod.Spec.SecurityContext = localContext(tenants[1].Name + ".json")
		Expect(owners[1].Create(ctx, bPod)).To(Succeed())
		verify(bPod, "Localhost")
		wrong := newPod(isolated, "a-profile")
		wrong.Spec.SecurityContext = localContext(tenants[0].Name + ".json")
		Expect(owners[1].Create(ctx, wrong)).To(MatchError(ContainSubstring("seccomp profile")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(wrong), &corev1.Pod{}))).To(BeTrue())
		cross := newPod(isolated, "cross-tenant")
		Expect(owners[0].Create(ctx, cross)).To(MatchError(ContainSubstring("cannot create resource \"pods\"")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.Pod{}))).To(BeTrue())
		By("validating controller templates on create and update")
		missing := MakeDeployment(selected.Name, "missing-profiles", 0, map[string]string{"env": "e2e"}, "")
		missing.Spec.Template.Spec.SecurityContext.SeccompProfile = nil
		Expect(owners[0].Create(ctx, missing)).To(MatchError(And(ContainSubstring("seccomp profile"), ContainSubstring("Unset"))))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(missing), &appsv1.Deployment{}))).To(BeTrue())
		deployment := MakeDeployment(selected.Name, "profile-template", 0, map[string]string{"env": "e2e"}, "")
		deployment.Spec.Template.Spec.SecurityContext = good.Spec.SecurityContext.DeepCopy()
		Expect(owners[0].Create(ctx, deployment)).To(Succeed())
		Eventually(func(g Gomega) {
			stored := &appsv1.Deployment{}
			g.Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(deployment), stored)).To(Succeed())
			stored.Spec.Template.Spec.SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
			err := owners[0].Update(ctx, stored)
			if err == nil {
				Fail("forbidden template update succeeded")
			}
			g.Expect(err).To(MatchError(And(ContainSubstring("spec.template"), ContainSubstring("seccomp profile"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedDeployment := &appsv1.Deployment{}
		Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(deployment), storedDeployment)).To(Succeed())
		Expect(storedDeployment.Spec.Template.Spec.SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeRuntimeDefault))
		By("validating newly added ephemeral containers")
		DeferCleanup(GrantEphemeralContainersUpdate(selected.Name, tenants[0].Name))
		cs := ownerClient(tenants[0].Spec.Owners[0].UserSpec)
		for _, denied := range []bool{true, false} {
			Eventually(func(g Gomega) {
				current, err := cs.CoreV1().Pods(selected.Name).Get(ctx, good.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				security := restrictedContainerSecurityContext()
				security.SeccompProfile = nil
				if denied {
					security.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
				}
				current.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", Image: "registry.k8s.io/pause:3.10", SecurityContext: security}}}
				_, err = cs.CoreV1().Pods(selected.Name).UpdateEphemeralContainers(ctx, current.Name, current, metav1.UpdateOptions{})
				if denied {
					if err == nil {
						Fail("forbidden ephemeral container was stored")
					}
					g.Expect(err).To(MatchError(And(ContainSubstring("ephemeralContainers[0]"), ContainSubstring("seccomp profile"))))
				} else {
					g.Expect(err).NotTo(HaveOccurred())
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			current, err := cs.CoreV1().Pods(selected.Name).Get(ctx, good.Name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			if denied {
				Expect(current.Spec.EphemeralContainers).To(BeEmpty())
			} else {
				Expect(current.Spec.EphemeralContainers).To(HaveLen(1))
			}
		}
		By("changing a namespace profile as administrator")
		Eventually(func(g Gomega) {
			ns := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(other), ns)).To(Succeed())
			ns.Labels["security-profile"] = "default"
			g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(other, 1, false)
		added := newPod(other, "new-profile")
		Expect(owners[0].Create(ctx, added)).To(Succeed())
		verify(added, "RuntimeDefault")
		blocked := newPod(other, "new-profile-blocked")
		blocked.Spec.Containers[0].SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
		Expect(owners[0].Create(ctx, blocked)).To(MatchError(ContainSubstring("seccomp profile")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(blocked), &corev1.Pod{}))).To(BeTrue())
		By("re-evaluating policy conditions without affecting the other tenant")
		Eventually(func(g Gomega) {
			current := &capsule.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[0].Enforce.Conditions = []rules.AdmissionCondition{{Expression: "false"}}
			g.Expect(k8sClient.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitProfile(selected, 1, true)
		after := newPod(selected, "after-policy-change")
		after.Spec.Containers[0].SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
		Expect(owners[0].Create(ctx, after)).To(Succeed())
		Eventually(func(g Gomega) {
			stored := &corev1.Pod{}
			g.Expect(owners[0].Get(ctx, client.ObjectKeyFromObject(after), stored)).To(Succeed())
			g.Expect(stored.Spec.Containers[0].SecurityContext.SeccompProfile.Type).To(Equal(corev1.SeccompProfileTypeUnconfined))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		stillEnforced := newPod(isolated, "after-policy-change")
		stillEnforced.Spec.Containers[0].SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
		Expect(owners[1].Create(ctx, stillEnforced)).To(MatchError(ContainSubstring("seccomp profile")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(stillEnforced), &corev1.Pod{}))).To(BeTrue())
	})
})
