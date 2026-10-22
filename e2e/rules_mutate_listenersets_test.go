// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("ListenerSet parent reference namespace profiles", Label("tenant", "rules", "mutation", "ingress", "listenersets"), func() {
	It("defaults and replaces complete parent references while preserving enforcement and tenant isolation", func() {
		ctx := context.Background()
		prefix := "e2e-lset-" + rand.String(8)
		var tenants []*capsulev1beta2.Tenant
		var owners []client.Client
		role := &rbacv1.ClusterRole{Name: prefix, Labels: map[string]string{"env": "e2e"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"gateway.networking.k8s.io"}, Resources: []string{"listenersets"}, Verbs: []string{"get", "create", "update", "delete"}}}}
		Expect(k8sClient.Create(ctx, role)).To(Succeed())
		DeferCleanup(EventuallyDeletion, role)
		mutation := func(name string, action rules.MutationAction, namespace *string) rules.NamespaceRuleMutation {
			return rules.NamespaceRuleMutation{Action: action, Ingress: rules.IngressMutation{ListenerSets: rules.ListenerSetMutation{ParentRef: &rules.GatewayParentReference{Name: name, Namespace: namespace}}}}
		}
		const gatewayNamespaceLabel = "networking.example.com/default-gateway-namespace"
		const gatewayNamespaceTemplate = `{{ index .tenant.metadata.labels "networking.example.com/default-gateway-namespace" }}`
		Expect(len(gatewayNamespaceTemplate)).To(BeNumerically(">", 63))
		for _, suffix := range []string{"a", "b"} {
			name := prefix + "-" + suffix
			selected := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "gateway-profile", Operator: metav1.LabelSelectorOpIn, Values: []string{"merge", "replace"}}}}
			defaultMutation := mutation("{{ .tenant.metadata.name }}-edge", "", new(gatewayNamespaceTemplate))
			defaultMutation.Conditions = []rules.AdmissionCondition{{Expression: `!has(object.metadata.labels) || !('skip-default' in object.metadata.labels)`}}
			forcedMutation := mutation(name+"-forced", rules.MutationActionReplace, nil)
			forcedMutation.Ingress.ListenerSets.ParentRef.Group = new("gateway.networking.k8s.io")
			forcedMutation.Ingress.ListenerSets.ParentRef.Kind = new("Gateway")
			tnt := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e", gatewayNamespaceLabel: "platform-gateways-" + suffix}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Kind: "User", Name: name}},
				Rules: []*rules.NamespaceRuleBodyTenant{
					{Permissions: rules.NamespaceRulePermissionBody{Bindings: []rbac.AdditionalRoleBindingsSpec{{ClusterRoleName: role.Name, Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: name}}}}}},
					{NamespaceSelector: selected, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
						Mutate: []rules.NamespaceRuleMutation{defaultMutation, {Conditions: []rules.AdmissionCondition{{Expression: `object.spec.containers[0].name == 'app'`}}, Workloads: rules.WorkloadMutation{Placement: rules.WorkloadPlacementMutation{Scheduler: "app"}}}},
						Enforce: &rules.NamespaceRuleEnforceBody{Action: rules.ActionTypeAllow,
							Ingress:  rules.NamespaceRuleEnforceIngressBody{Types: []rules.IngressType{rules.IngressTypeListenerSet}, Hostnames: []apiruntime.ExpressionMatch{{Exact: []string{"app.example.com"}}}},
							Metadata: []rules.MetadataRule{{APIGroups: []string{"gateway.networking.k8s.io/v1"}, Kinds: []string{"ListenerSet"}, Labels: map[string]rules.MetadataValueRule{"profile-default": {Default: new("applied")}}}},
						},
					}},
					{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"gateway-profile": "replace"}}, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Mutate: []rules.NamespaceRuleMutation{forcedMutation}}},
					{NamespaceSelector: selected, NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Audience: []rules.Audience{{Kind: rules.AudienceKindUser, Name: "unrelated-user"}}, Mutate: []rules.NamespaceRuleMutation{mutation("wrong-audience", rules.MutationActionReplace, nil)}}},
				},
			}}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			stored := &capsulev1beta2.Tenant{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tnt), stored)).To(Succeed())
			ref := stored.Spec.Rules[1].Mutate[0].Ingress.ListenerSets.ParentRef
			Expect(ref.Group).NotTo(BeNil())
			Expect(*ref.Group).To(Equal("gateway.networking.k8s.io"))
			Expect(ref.Kind).NotTo(BeNil())
			Expect(*ref.Kind).To(Equal("Gateway"))
			Expect(*ref.Namespace).To(Equal(gatewayNamespaceTemplate))
			tenants = append(tenants, tnt)
			owners = append(owners, impersonationClient(name, withDefaultGroups(nil)))
		}
		expectProfile := func(ns *corev1.Namespace, count int, name string) {
			Eventually(func(g Gomega) {
				status := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(status.Status.Rules).To(HaveLen(count))
				if count > 0 {
					g.Expect(status.Status.Rules[0].Mutate[0].Ingress.ListenerSets.ParentRef.Name).To(Equal(name))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		newNamespace := func(tenant int, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tenants[tenant].Name, "gateway-profile": profile})
			NamespaceCreation(ns, tenants[tenant].Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tenants[tenant], ns).Should(Succeed())
			Eventually(func() bool {
				probe := &unstructured.Unstructured{}
				probe.SetAPIVersion("gateway.networking.k8s.io/v1")
				probe.SetKind("ListenerSet")
				return apierrors.IsNotFound(owners[tenant].Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "rbac-readiness"}, probe))
			}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
			count := 0
			if profile == "merge" {
				count = 2
			}
			if profile == "replace" {
				count = 3
			}
			expectProfile(ns, count, tenants[tenant].Name+"-edge")
			return ns
		}
		mergeNS, replaceNS := newNamespace(0, "merge"), newNamespace(0, "replace")
		otherNS, tenantBNS := newNamespace(0, "other"), newNamespace(1, "merge")
		listenerSet := func(ns *corev1.Namespace, name string, ref map[string]any) *unstructured.Unstructured {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "gateway.networking.k8s.io/v1", "kind": "ListenerSet",
				"metadata": map[string]any{"name": name, "namespace": ns.Name, "labels": map[string]any{"env": "e2e"}},
				"spec":     map[string]any{"listeners": []any{map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP", "hostname": "app.example.com"}}},
			}}
			if ref != nil {
				Expect(unstructured.SetNestedMap(obj.Object, ref, "spec", "parentRef")).To(Succeed())
			}
			return obj
		}
		parentRef := func(name string, namespace *string) map[string]any {
			ref := map[string]any{"name": name, "group": "gateway.networking.k8s.io", "kind": "Gateway"}
			if namespace != nil {
				ref["namespace"] = *namespace
			}
			return ref
		}
		expectRef := func(obj *unstructured.Unstructured, want map[string]any, metadata bool) {
			Eventually(func(g Gomega) {
				stored := obj.DeepCopy()
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
				ref, _, err := unstructured.NestedMap(stored.Object, "spec", "parentRef")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(ref).To(Equal(want))
				if metadata {
					g.Expect(stored.GetLabels()).To(HaveKeyWithValue("profile-default", "applied"))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		create := func(owner client.Client, ns *corev1.Namespace, name string, supplied, want map[string]any, metadata bool) *unstructured.Unstructured {
			obj := listenerSet(ns, name, supplied)
			Expect(owner.Create(ctx, obj)).To(Succeed())
			expectRef(obj, want, metadata)
			return obj
		}
		By("defaulting omitted references and preserving explicit local references atomically")
		defaultRef := parentRef(tenants[0].Name+"-edge", new(tenants[0].Labels[gatewayNamespaceLabel]))
		original := create(owners[0], mergeNS, "omitted", nil, defaultRef, true)
		create(owners[0], mergeNS, "explicit", map[string]any{"name": "custom"}, parentRef("custom", nil), true)
		create(owners[0], replaceNS, "replace", map[string]any{"name": "custom", "namespace": "old"}, parentRef(tenants[0].Name+"-forced", nil), true)
		create(owners[0], otherNS, "unselected", map[string]any{"name": "custom"}, parentRef("custom", nil), false)
		create(owners[1], tenantBNS, "tenant-b", nil, parentRef(tenants[1].Name+"-edge", new(tenants[1].Labels[gatewayNamespaceLabel])), true)

		By("rejecting invalid rendered namespaces in effective rules without changing the active profile")
		for _, namespace := range []string{"invalid.namespace", strings.Repeat("a", 64)} {
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: mergeNS.Name, Name: meta.NameForManagedRuleStatus()}, current)).To(Succeed())
				current.Spec[0].Mutate[0].Ingress.ListenerSets.ParentRef.Namespace = new(namespace)
				err := k8sClient.Update(ctx, current)
				if err == nil {
					Fail("invalid rendered Gateway namespace was accepted")
				}
				g.Expect(err).To(MatchError(ContainSubstring("ingress.listenerSets.parentRef.namespace")))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			stored := &capsulev1beta2.RuleStatus{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: mergeNS.Name, Name: meta.NameForManagedRuleStatus()}, stored)).To(Succeed())
			Expect(*stored.Spec[0].Mutate[0].Ingress.ListenerSets.ParentRef.Namespace).To(Equal(tenants[0].Labels[gatewayNamespaceLabel]))
		}
		expectRef(original, defaultRef, true)

		By("retaining conditions and hostname enforcement after mutation")
		skipped := listenerSet(mergeNS, "conditional", map[string]any{"name": "custom"})
		skipped.SetLabels(map[string]string{"env": "e2e", "skip-default": "true"})
		Expect(owners[0].Create(ctx, skipped)).To(Succeed())
		expectRef(skipped, parentRef("custom", nil), true)
		conditionalMissing := listenerSet(mergeNS, "conditional-missing", nil)
		conditionalMissing.SetLabels(map[string]string{"env": "e2e", "skip-default": "true"})
		err := owners[0].Create(ctx, conditionalMissing)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("parentRef")))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(conditionalMissing), conditionalMissing.DeepCopy()))).To(BeTrue())
		for _, tc := range []struct {
			name   string
			obj    *unstructured.Unstructured
			reason string
		}{
			{"hostname", listenerSet(mergeNS, "denied-hostname", nil), "hostname"},
			{"unselected", listenerSet(otherNS, "missing-parent", nil), "parentRef"},
			{"malformed", listenerSet(mergeNS, "malformed-parent", map[string]any{"namespace": "platform-gateways"}), "name"},
		} {
			if tc.name == "hostname" {
				Expect(unstructured.SetNestedSlice(tc.obj.Object, []any{map[string]any{"name": "http", "port": int64(80), "protocol": "HTTP", "hostname": "forbidden.example.com"}}, "spec", "listeners")).To(Succeed())
			}
			err := owners[0].Create(ctx, tc.obj)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(ContainSubstring(tc.reason)))
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(tc.obj), tc.obj.DeepCopy()))).To(BeTrue())
		}
		crossTenant := listenerSet(tenantBNS, "cross-tenant", nil)
		err = owners[0].Create(ctx, crossTenant)
		Expect(apierrors.IsForbidden(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring(`cannot create resource "listenersets"`)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(crossTenant), crossTenant.DeepCopy()))).To(BeTrue())

		By("defaulting references removed on UPDATE without rewriting existing parent choices")
		Eventually(func() error {
			current := original.DeepCopy()
			if err := owners[0].Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
				return err
			}
			unstructured.RemoveNestedField(current.Object, "spec", "parentRef")
			return owners[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectRef(original, defaultRef, true)

		By("rejecting invalid policy updates and preserving the active rule")
		Eventually(func(g Gomega) {
			current := &capsulev1beta2.Tenant{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current)).To(Succeed())
			current.Spec.Rules[1].Mutate[0].Ingress.ListenerSets.ParentRef.Name = "bad/name"
			err := k8sClient.Update(ctx, current)
			if err == nil {
				Fail("invalid parent reference policy was accepted")
			}
			g.Expect(err).To(MatchError(ContainSubstring("ingress.listenerSets.parentRef.name")))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		storedPolicy := &capsulev1beta2.Tenant{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), storedPolicy)).To(Succeed())
		Expect(storedPolicy.Spec.Rules[1].Mutate[0].Ingress.ListenerSets.ParentRef.Name).To(Equal("{{ .tenant.metadata.name }}-edge"))
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenants[0]), current); err != nil {
				return err
			}
			current.Spec.Rules[1].Mutate[0].Ingress.ListenerSets.ParentRef.Name = "updated-edge"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectProfile(mergeNS, 2, "updated-edge")
		create(owners[0], mergeNS, "updated-policy", nil, parentRef("updated-edge", new(tenants[0].Labels[gatewayNamespaceLabel])), true)
		expectRef(original, defaultRef, true)
		create(owners[1], tenantBNS, "tenant-b-unchanged", nil, parentRef(tenants[1].Name+"-edge", new(tenants[1].Labels[gatewayNamespaceLabel])), true)

		By("changing namespace profiles without reconciling existing ListenerSets")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mergeNS), current); err != nil {
				return err
			}
			current.Labels["gateway-profile"] = "replace"
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectProfile(mergeNS, 3, "updated-edge")
		expectRef(original, defaultRef, true)
		Eventually(func() error {
			current := original.DeepCopy()
			if err := owners[0].Get(ctx, client.ObjectKeyFromObject(original), current); err != nil {
				return err
			}
			labels := current.GetLabels()
			labels["updated"] = "true"
			current.SetLabels(labels)
			return owners[0].Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		expectRef(original, parentRef(tenants[0].Name+"-forced", nil), true)
	})
})
