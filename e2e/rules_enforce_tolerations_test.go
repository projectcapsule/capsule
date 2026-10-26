// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("toleration enforcement", Label("tenant", "rules", "enforce", "workloads", "placement", "tolerations"), func() {
	ctx := context.Background()
	var tenants []*capsulev1beta2.Tenant

	BeforeEach(func() {
		prefix := "e2e-tolerations-" + rand.String(6)
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e"}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Name: name, Kind: "User"}},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
		}
	})
	AfterEach(func() { tenants = nil })

	ruleFromYAML := func(matcher string) *rules.NamespaceRuleBodyTenant {
		var spec capsulev1beta2.TenantSpec
		Expect(yaml.UnmarshalStrict([]byte(`rules:
  - enforce:
      action: deny
      workloads:
        placement:
          tolerations:
            `+matcher+"\n"), &spec)).To(Succeed())
		return spec.Rules[0]
	}
	setRules := func(tnt *capsulev1beta2.Tenant, desired ...*rules.NamespaceRuleBodyTenant) {
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current); err != nil {
				return err
			}
			current.Spec.Rules = desired
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		current := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), current)).To(Succeed())
		Expect(current.Spec.Rules).To(Equal(desired), "the API server must preserve toleration matchers in Tenant.spec.rules")
	}
	newNamespace := func(tnt *capsulev1beta2.Tenant, selected bool) *corev1.Namespace {
		labels := map[string]string{meta.TenantLabel: tnt.Name}
		if selected {
			labels["toleration-profile"] = "restricted"
		}
		ns := NewNamespace("", labels)
		NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "default"}, &corev1.ServiceAccount{})
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		return ns
	}
	expectRules := func(ns *corev1.Namespace, expected ...*rules.NamespaceRuleBodyNamespace) {
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
			ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(status.Spec).To(HaveLen(len(expected)))
			g.Expect(status.Status.Rules).To(HaveLen(len(expected)))
			for i, body := range expected {
				g.Expect(status.Spec[i]).To(Equal(body))
				g.Expect(status.Status.Rules[i]).To(Equal(body), "toleration matchers must reach the effective RuleStatus")
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	podFor := func(ns *corev1.Namespace, key string) *corev1.Pod {
		return &corev1.Pod{Name: "toleration-" + rand.String(8), Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}, Spec: corev1.PodSpec{
			SchedulingGates: []corev1.PodSchedulingGate{{Name: "example.com/e2e-tolerations"}},
			Tolerations:     []corev1.Toleration{{Key: key, Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute}},
			SecurityContext: nobodyPodSecurityContext(), Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
		}}
	}
	checkPod := func(tnt *capsulev1beta2.Tenant, ns *corev1.Namespace, key string, allowed bool) *corev1.Pod {
		owner := impersonationClient(tnt.Spec.Owners[0].Name, withDefaultGroups(nil))
		pod := podFor(ns, key)
		err := owner.Create(ctx, pod)
		if allowed {
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				stored := &corev1.Pod{}
				g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(pod), stored)).To(Succeed())
				g.Expect(stored.Spec.Tolerations).To(ContainElement(corev1.Toleration{Key: key, Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		} else {
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected Capsule toleration denial: %v", err)
			Expect(err).To(MatchError(And(ContainSubstring("toleration"), ContainSubstring("spec.tolerations["), ContainSubstring(key))))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
			fmt.Fprintf(GinkgoWriter, "Denied %s/%s: %v\n", ns.Name, pod.Name, err)
		}
		return pod
	}

	It("preserves toleration-only rules through Tenant and RuleStatus and enforces namespace profiles and updates", func() {
		a, b := tenants[0], tenants[1]
		selected, plain, other := newNamespace(a, true), newNamespace(a, false), newNamespace(b, true)
		TenantNamespaceReady(a, selected, 2)
		TenantNamespaceReady(a, plain, 2)
		TenantNamespaceReady(b, other, 1)

		By("reproducing the reported toleration with the original deny-all rule")
		all := ruleFromYAML("- {}")
		setRules(a, all)
		expectRules(selected, all.NamespaceRuleBodyNamespace)
		expectRules(plain, all.NamespaceRuleBodyNamespace)
		checkPod(a, selected, "border", false)
		checkPod(a, selected, "other", false)
		checkPod(a, plain, "border", false)
		checkPod(b, other, "border", true)

		By("matching listed keys under placement without affecting other namespace profiles or tenants")
		listed := ruleFromYAML("- key: {exact: [border, node-role.kubernetes.io/master, node-role.kubernetes.io/control-plane], negate: false}")
		listed.NamespaceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"toleration-profile": "restricted"}}
		service := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
			Action: rules.ActionTypeDeny, Services: rules.NamespaceRuleEnforceServicesBody{Types: []rules.ServiceType{rules.ServiceTypeNodePort}},
		}}}
		setRules(a, service, listed)
		expectRules(selected, service.NamespaceRuleBodyNamespace, listed.NamespaceRuleBodyNamespace)
		expectRules(plain, service.NamespaceRuleBodyNamespace)
		expectRules(other)
		for _, key := range []string{"border", "node-role.kubernetes.io/master", "node-role.kubernetes.io/control-plane"} {
			checkPod(a, selected, key, false)
		}
		allowed := checkPod(a, selected, "other", true)
		checkPod(a, plain, "border", true)
		checkPod(b, other, "border", true)
		cross := podFor(other, "border")
		ownerA := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		err := ownerA.Create(ctx, cross)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "pods"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), &corev1.Pod{}))).To(BeTrue())

		By("rejecting forbidden tolerations added on Pod updates without persisting them")
		owner := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		Eventually(func(g Gomega) {
			current := &corev1.Pod{}
			g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(allowed), current)).To(Succeed())
			current.Spec.Tolerations = append(current.Spec.Tolerations, corev1.Toleration{Key: "border", Operator: corev1.TolerationOpEqual, Value: "true", Effect: corev1.TaintEffectNoExecute})
			err := owner.Update(ctx, current)
			if err == nil {
				Fail("a forbidden toleration was accepted on Pod update")
			}
			g.Expect(apierrors.IsForbidden(err)).To(BeTrue())
			g.Expect(err).To(MatchError(And(ContainSubstring("spec.tolerations["), ContainSubstring("border"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		stored := &corev1.Pod{}
		Expect(owner.Get(ctx, client.ObjectKeyFromObject(allowed), stored)).To(Succeed())
		for _, toleration := range stored.Spec.Tolerations {
			Expect(toleration.Key).NotTo(Equal("border"))
		}

		By("reconciling profile selection after a namespace label update")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(plain), current); err != nil {
				return err
			}
			current.Labels["toleration-profile"] = "restricted"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectRules(plain, service.NamespaceRuleBodyNamespace, listed.NamespaceRuleBodyNamespace)
		checkPod(a, plain, "border", false)
		checkPod(b, other, "border", true)
	})

	It("rejects the reported flat toleration forms with strict field validation and preserves the valid policy", func() {
		a := tenants[0]
		valid := ruleFromYAML("- key: {exact: [border], negate: false}")
		setRules(a, valid)
		ns := newNamespace(a, true)
		TenantNamespaceReady(a, ns, 1)
		expectRules(ns, valid.NamespaceRuleBodyNamespace)
		for _, matcher := range []map[string]any{
			{},
			{"key": map[string]any{"exact": []any{"border", "node-role.kubernetes.io/master", "node-role.kubernetes.io/control-plane"}, "negate": false}},
		} {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.Tenant{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current)).To(Succeed())
				object, err := runtime.DefaultUnstructuredConverter.ToUnstructured(current)
				g.Expect(err).NotTo(HaveOccurred())
				raw := &unstructured.Unstructured{Object: object}
				raw.SetGroupVersionKind(capsulev1beta2.GroupVersion.WithKind("Tenant"))
				entries, found, err := unstructured.NestedSlice(raw.Object, "spec", "rules")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				entries[0].(map[string]any)["enforce"].(map[string]any)["workloads"] = map[string]any{"tolerations": []any{matcher}}
				g.Expect(unstructured.SetNestedSlice(raw.Object, entries, "spec", "rules")).To(Succeed())
				err = k8sClient.Update(ctx, raw, client.FieldValidation(metav1.FieldValidationStrict))
				if err == nil {
					Fail("unsupported flat toleration rules were accepted with strict field validation")
				}
				g.Expect(err).To(MatchError(ContainSubstring(`unknown field "spec.rules[0].enforce.workloads.tolerations"`)))
				fmt.Fprintf(GinkgoWriter, "Rejected flat toleration rule: %v\n", err)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		stored := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(a), stored)).To(Succeed())
		Expect(stored.Spec.Rules).To(Equal([]*rules.NamespaceRuleBodyTenant{valid}))
		expectRules(ns, valid.NamespaceRuleBodyNamespace)
		checkPod(a, ns, "border", false)
		checkPod(a, ns, "other", true)
	})
})
