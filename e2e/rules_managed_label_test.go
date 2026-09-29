// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/users"
)

var _ = Describe("managed labels cannot bypass metadata enforcement", Label("tenant", "rules", "metadata", "managed-label"), func() {
	const denied = "pod-security.kubernetes.io/enforce"
	const profile = "example.org/profile"
	ctx := context.Background()
	var tenantA, tenantB *capsulev1beta2.Tenant
	var selected, unselected, other *corev1.Namespace
	var owner, controller kubernetes.Interface

	newPod := func(name string, labels map[string]string) *corev1.Pod {
		return &corev1.Pod{Name: name, Labels: maps.Clone(labels), Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10.1"}},
		}}
	}
	expectDenied := func(err error, reasons ...string) {
		Expect(err).To(HaveOccurred(), "an unauthorized write must never succeed")
		Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected admission rejection: %v", err)
		matched := false
		for _, reason := range reasons {
			matched = matched || strings.Contains(err.Error(), reason)
		}
		Expect(matched).To(BeTrue(), "unexpected denial: %v", err)
	}

	BeforeEach(func() {
		createTenant := func(name string) *capsulev1beta2.Tenant {
			return &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e", "example.org/managed-label-tenant": name}, Spec: capsulev1beta2.TenantSpec{
				Owners: rbac.OwnerListSpec{{Name: name, Kind: rbac.UserOwner}},
			}}
		}
		prefix := "e2e-managed-label-" + rand.String(6)
		tenantA, tenantB = createTenant(prefix+"-a"), createTenant(prefix+"-b")
		tenantA.Spec.Rules = []*rules.NamespaceRuleBodyTenant{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{profile: "restricted"}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeDeny,
				Metadata: []rules.MetadataRule{{APIGroups: []string{"v1"}, Kinds: []string{"Namespace", "Pod", "ConfigMap"}, Labels: map[string]rules.MetadataValueRule{
					denied: {Values: []runtime.ExpressionMatch{{Exact: []string{"privileged"}}}},
				}}},
			}},
		}}
		for _, tnt := range []*capsulev1beta2.Tenant{tenantA, tenantB} {
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			EventuallyCreation(func() error { return k8sClient.Create(ctx, tnt) }).Should(Succeed())
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
		}
		selected = NewNamespace(tenantA.Name+"-selected", map[string]string{meta.TenantLabel: tenantA.Name, profile: "restricted"})
		unselected = NewNamespace(tenantA.Name+"-unselected", map[string]string{meta.TenantLabel: tenantA.Name})
		other = NewNamespace(tenantB.Name+"-ns", map[string]string{meta.TenantLabel: tenantB.Name, profile: "restricted"})
		for _, item := range []struct {
			ns  *corev1.Namespace
			tnt *capsulev1beta2.Tenant
		}{{selected, tenantA}, {unselected, tenantA}, {other, tenantB}} {
			NamespaceCreation(item.ns, item.tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		}
		TenantNamespaceReady(tenantA, selected, 2)
		TenantNamespaceReady(tenantA, unselected, 2)
		TenantNamespaceReady(tenantB, other, 1)
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.Rules).To(HaveLen(1))
			g.Expect(status.Status.Rules[0].Enforce).To(Equal(tenantA.Spec.Rules[0].Enforce))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		owner = ownerClient(tenantA.Spec.Owners[0].UserSpec)
		controller = impersonationClientSet(ControllerServiceAccountFull, users.ServiceAccountGroups(ControllerNamespace))
	})

	It("rejects forged exemptions on create and update while preserving controller writes and tenant profiles", func() {
		By("allowing ordinary owner writes and enforcing the deny rule without a managed label")
		pod, err := owner.CoreV1().Pods(selected.Name).Create(ctx, newPod("ordinary", nil), metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = owner.CoreV1().Pods(selected.Name).Create(ctx, newPod("baseline-denied", map[string]string{denied: "privileged"}), metav1.CreateOptions{})
		expectDenied(err, "denied by namespace rule")

		// Namespace owners have PATCH but need not have GET. Use the admin reader
		// for a fresh resourceVersion and perform the actual patch as the owner.
		patchNamespace := func(cs kubernetes.Interface, name string, labels map[string]any) error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, current); err != nil {
				return err
			}
			patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"resourceVersion": current.ResourceVersion, "labels": labels}})
			if err != nil {
				return err
			}
			_, err = cs.CoreV1().Namespaces().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
			return err
		}
		denyNamespacePatch := func(name string, labels map[string]any, reasons ...string) {
			Eventually(func(g Gomega) {
				err := patchNamespace(owner, name, labels)
				if apierrors.IsConflict(err) {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				expectDenied(err, reasons...)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		denyNamespacePatch(selected.Name, map[string]any{denied: "privileged"}, "denied by namespace rule")

		for _, value := range []string{meta.ValueController, meta.ValueControllerResources} {
			By(fmt.Sprintf("refusing forged managed-by=%s labels with and without forbidden metadata", value))
			for _, forbidden := range []bool{false, true} {
				labels := map[string]string{meta.NewManagedByCapsuleLabel: value}
				if forbidden {
					labels[denied] = "privileged"
				}
				name := fmt.Sprintf("forged-%s-%t", value, forbidden)
				_, err := owner.CoreV1().Pods(selected.Name).Create(ctx, newPod(name, labels), metav1.CreateOptions{})
				expectDenied(err, "Labeling resources as controller managed", "denied by namespace rule")
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: name}, &corev1.Pod{}))).To(BeTrue())

				ns := NewNamespace(tenantA.Name+"-"+name, labels, map[string]string{meta.TenantLabel: tenantA.Name, profile: "restricted"})
				_, err = owner.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				expectDenied(err, "Labeling resources as controller managed", "denied by namespace rule")
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: ns.Name}, &corev1.Namespace{}))).To(BeTrue())
				patch := map[string]any{meta.NewManagedByCapsuleLabel: value}
				if forbidden {
					patch[denied] = "privileged"
				}
				denyNamespacePatch(selected.Name, patch, "Labeling resources as controller managed", "denied by namespace rule")
			}
			Eventually(func(g Gomega) {
				current, err := owner.CoreV1().Pods(selected.Name).Get(ctx, pod.Name, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				current.Labels = map[string]string{denied: "privileged", meta.NewManagedByCapsuleLabel: value}
				_, err = owner.CoreV1().Pods(selected.Name).Update(ctx, current, metav1.UpdateOptions{})
				if apierrors.IsConflict(err) {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				expectDenied(err, "Labeling resources as controller managed", "denied by namespace rule")
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			current := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: pod.Name}, current)).To(Succeed())
			Expect(current.Labels).NotTo(HaveKey(denied))
			Expect(current.Labels).NotTo(HaveKey(meta.NewManagedByCapsuleLabel))
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: selected.Name}, ns)).To(Succeed())
			Expect(ns.Labels).NotTo(HaveKey(denied))
			Expect(ns.Labels).NotTo(HaveKey(meta.NewManagedByCapsuleLabel))

			By("preserving authenticated controller writes despite matching deny rules")
			created, err := controller.CoreV1().Pods(selected.Name).Create(ctx, newPod("controller-"+value, map[string]string{denied: "privileged", meta.NewManagedByCapsuleLabel: value}), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: selected.Name, Name: created.Name}, current)).To(Succeed())
			Expect(current.Labels).To(HaveKeyWithValue(denied, "privileged"))
			Eventually(func() error {
				return patchNamespace(controller, selected.Name, map[string]any{denied: "privileged", meta.NewManagedByCapsuleLabel: value})
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: selected.Name}, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue(denied, "privileged"))
			By("preventing an owner from removing a controller's managed marker")
			denyNamespacePatch(selected.Name, map[string]any{meta.NewManagedByCapsuleLabel: nil, denied: nil}, "Labeling resources as controller managed")
			Eventually(func() error {
				return patchNamespace(controller, selected.Name, map[string]any{denied: nil, meta.NewManagedByCapsuleLabel: nil})
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		By("keeping unrelated namespace profiles and tenants independent")
		for _, item := range []struct {
			ns *corev1.Namespace
			cs kubernetes.Interface
		}{{unselected, owner}, {other, ownerClient(tenantB.Spec.Owners[0].UserSpec)}} {
			created, err := item.cs.CoreV1().Pods(item.ns.Name).Create(ctx, newPod("different-profile", map[string]string{denied: "privileged"}), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			current := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: item.ns.Name, Name: created.Name}, current)).To(Succeed())
			Expect(current.Labels).To(HaveKeyWithValue(denied, "privileged"))
		}
		denyNamespacePatch(other.Name, map[string]any{"example.org/hijacked": "true"}, "denied patch request for this namespace")
		current := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: other.Name}, current)).To(Succeed())
		Expect(current.Labels).NotTo(HaveKey("example.org/hijacked"))
	})

	DescribeTable("enforces metadata on managed targets and retains their protection", func(source string) {
		name := "e2e-managed-label-" + source + "-" + rand.String(6)
		for _, sa := range []string{name, name + "-other"} {
			DeferCleanup(func() {
				EventuallyDeletion(&rbacv1.ClusterRoleBinding{Name: sa + "-namespaces-binding"})
				EventuallyDeletion(&rbacv1.ClusterRole{Name: sa + "-namespaces"})
			})
		}
		grantResourcePermitServiceAccount(selected.Name, name, selected.Name, []string{"get", "list", "watch", "create", "update", "patch", "delete"})
		runner := impersonationClient(serviceAccountUsername(selected.Name, name), serviceAccountGroups(selected.Name))
		// Give the second identity Kubernetes access to this target so rejection
		// demonstrates the managed-resource guard, rather than missing RBAC.
		grantResourcePermitServiceAccount(other.Name, name+"-other", selected.Name, []string{"get", "patch", "update"})
		otherRunner := impersonationClient(serviceAccountUsername(other.Name, name+"-other"), serviceAccountGroups(other.Name))
		cm := &corev1.ConfigMap{Name: name, Namespace: selected.Name}
		protection, protectionReason := meta.ResourcePermitProtectionLabel, "resources protected by a ResourcePermit"
		By("creating a real managed target through its execution ServiceAccount")
		if source == meta.ValueControllerResourcePermit {
			template := &capsulev1beta2.GlobalResourcePermitTemplate{Name: name, Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
				Impersonation: resourcePermitServiceAccountReference(selected.Name, name),
				Approvals:     resourcepermit.ApprovalSpec{Auto: true},
				Resources: []runtime.ResourceTemplate{{Template: fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: %s
data:
  source: resource-permit
`, name)}},
			}}
			DeferCleanup(func() { EventuallyDeletion(template) })
			EventuallyCreation(func() error { return k8sClient.Create(ctx, template) }).Should(Succeed())
			permit := newImpersonatedResourcePermit(selected.Name, name, template.Name)
			DeferCleanup(func() {
				expireResourcePermitForCleanup(ctx, permit)
				EventuallyDeletion(permit)
			})
			EventuallyCreation(func() error { return k8sClient.Create(ctx, permit) }).Should(Succeed())
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.ResourcePermit{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(permit), current)).To(Succeed())
				g.Expect(current.Status.Phase).To(Equal(capsulev1beta2.ResourcePermitPhaseActive))
				expectResourcePermitServiceAccount(g, current, selected.Name, name)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		} else {
			protection, protectionReason = meta.ReplicationProtectionLabel, "is managed by a global capsule replication"
			parent := &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope:          api.ResourceScopeNamespace,
				TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"example.org/managed-label-tenant": tenantA.Name}},
				ServiceAccount: resourcePermitServiceAccountReference(selected.Name, name),
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{Resources: []capsulev1beta2.ResourceSpec{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": selected.Name}},
					Policy:            &runtime.ResourceReplicationPolicy{Protect: new(true)},
					RawItems: []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{
						APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"source": source},
					}}},
				}}},
			}}
			DeferCleanup(func() { EventuallyDeletion(parent) })
			EventuallyCreation(func() error { return k8sClient.Create(ctx, parent) }).Should(Succeed())
			Eventually(func(g Gomega) {
				current := &capsulev1beta2.GlobalTenantResource{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), current)).To(Succeed())
				g.Expect(current.Status.ProcessedItems).To(HaveLen(1))
				g.Expect(current.Status.ProcessedItems[0].Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
			g.Expect(cm.Labels).To(HaveKeyWithValue(meta.NewManagedByCapsuleLabel, source))
			g.Expect(cm.Labels).To(HaveKeyWithValue(protection, meta.ValueTrue))
			g.Expect(cm.Data).To(HaveKeyWithValue("source", source))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("allowing and persisting a valid update by the target's execution identity")
		Eventually(func(g Gomega) {
			current := &corev1.ConfigMap{}
			g.Expect(runner.Get(ctx, client.ObjectKeyFromObject(cm), current)).To(Succeed())
			if current.Annotations == nil {
				current.Annotations = map[string]string{}
			}
			current.Annotations["example.org/valid"] = "allowed"
			g.Expect(runner.Update(ctx, current)).To(Succeed())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
		Expect(cm.Annotations).To(HaveKeyWithValue("example.org/valid", "allowed"))

		for _, marker := range []string{source, meta.ValueController, meta.ValueControllerResources} {
			By("enforcing metadata rules even for the authorized execution identity with managed-by=" + marker)
			Eventually(func(g Gomega) {
				current := &corev1.ConfigMap{}
				g.Expect(runner.Get(ctx, client.ObjectKeyFromObject(cm), current)).To(Succeed())
				current.Labels[denied], current.Labels[meta.NewManagedByCapsuleLabel] = "privileged", marker
				err := runner.Update(ctx, current)
				if apierrors.IsConflict(err) {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				if marker == source {
					expectDenied(err, "denied by namespace rule")
				} else {
					expectDenied(err, "denied by namespace rule", "Labeling resources as controller managed")
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		By("rejecting removal of all protection markers by the owner or another tenant's execution identity")
		for _, actor := range []client.Client{impersonationClient(tenantA.Spec.Owners[0].Name, withDefaultGroups(nil)), otherRunner} {
			Eventually(func(g Gomega) {
				current := &corev1.ConfigMap{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), current)).To(Succeed())
				current.Labels = nil
				current.Annotations = map[string]string{meta.ResourcePermitServiceAccountAnnotation: serviceAccountUsername(other.Name, name+"-other")}
				err := actor.Update(ctx, current)
				if apierrors.IsConflict(err) {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				expectDenied(err, protectionReason)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
		Expect(cm.Labels).NotTo(HaveKey(denied))
		Expect(cm.Labels).To(HaveKeyWithValue(meta.NewManagedByCapsuleLabel, source))
		Expect(cm.Labels).To(HaveKeyWithValue(protection, meta.ValueTrue))
		Expect(cm.Annotations).To(HaveKeyWithValue("example.org/valid", "allowed"))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: other.Name, Name: name}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("ResourcePermit", Label("resource-permit"), meta.ValueControllerResourcePermit), Entry("GlobalTenantResource", Label("replications"), meta.ValueControllerReplications))
})
