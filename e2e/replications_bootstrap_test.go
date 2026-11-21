// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("Replication namespace bootstrap", Label("tenant", "replications", "replication-bootstrap"), func() {
	DescribeTable("bootstraps profile changes and retains cleanup during full reconciles", Label("bootstrap-review"), func(global bool) {
		ctx := context.Background()
		prefix := "bootstrap-review-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		bindServiceAccountToTenantResourceManager(namespaces[0].Name, "default", namespaces[1].Name)
		spec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-profile": "selected"}},
			Policy:            &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Protect: new(true)},
			RawItems:          []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"tenant":"a"}}`, prefix))}},
		}}}
		extra := *spec.Resources[0].DeepCopy()
		extra.NamespaceSelector.MatchLabels["projectcapsule.dev/watch-extra"] = "selected"
		extra.RawItems = []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"tenant":"a"}}`, prefix+"-extra"))}}
		spec.Resources = append(spec.Resources, extra)
		parent := createHealthReviewReplication(global, prefix, namespaces[0].Name, spec, actor)
		waitWatchReady(parent)
		// Namespace profiles are administered at cluster scope. Tenant clients
		// below exercise access to the resulting replicated resources.
		setNamespaceLabel := func(namespace, label, value string) {
			Eventually(func() error {
				fresh := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: namespace}, fresh); err != nil {
					return err
				}
				fresh.Labels[label] = value
				return k8sClient.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		selectNamespace := func(namespace string, selected bool) {
			value := "excluded"
			if selected {
				value = "selected"
			}
			setNamespaceLabel(namespace, "projectcapsule.dev/watch-profile", value)
		}
		By("selecting an existing namespace without a full reconcile request")
		selectNamespace(namespaces[2].Name, true)
		selectNamespace(namespaces[1].Name, true)
		key := client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix}
		Eventually(func(g Gomega) {
			target := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, key, target)).To(Succeed())
			g.Expect(target.Data).To(HaveKeyWithValue("tenant", "a"))
			fresh, status := replicationHealthObject(parent)
			g.Expect(status.ProcessedItems).To(HaveLen(2))
			g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
			g.Expect(fresh.GetFinalizers()).To(ContainElement(meta.ControllerFinalizer))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("keeping owner writes denied for the protected replica")
		target := &corev1.ConfigMap{}
		Expect(actor.Get(ctx, key, target)).To(Succeed())
		Expect(actor.Patch(ctx, target, client.RawPatch(types.MergePatchType, []byte(`{"data":{"tenant":"changed"}}`)))).To(MatchError(ContainSubstring("capsule replication")))
		Expect(actor.Get(ctx, key, target)).To(Succeed())
		Expect(target.Data).To(HaveKeyWithValue("tenant", "a"))
		By("pruning only the resource block that loses its namespace match")
		setNamespaceLabel(namespaces[2].Name, "projectcapsule.dev/watch-extra", "selected")
		setNamespaceLabel(namespaces[1].Name, "projectcapsule.dev/watch-extra", "selected")
		extraKey := client.ObjectKey{Namespace: namespaces[1].Name, Name: prefix + "-extra"}
		Eventually(func(g Gomega) {
			g.Expect(actor.Get(ctx, extraKey, &corev1.ConfigMap{})).To(Succeed())
			_, status := replicationHealthObject(parent)
			g.Expect(status.ProcessedItems).To(HaveLen(3))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		setNamespaceLabel(namespaces[1].Name, "projectcapsule.dev/watch-extra", "excluded")
		Eventually(func(g Gomega) {
			g.Expect(apierrors.IsNotFound(actor.Get(ctx, extraKey, &corev1.ConfigMap{}))).To(BeTrue())
			g.Expect(actor.Get(ctx, key, &corev1.ConfigMap{})).To(Succeed())
			_, status := replicationHealthObject(parent)
			g.Expect(status.ProcessedItems).To(HaveLen(2))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("pruning a deselected namespace without a manual reconcile annotation")
		selectNamespace(namespaces[1].Name, false)
		Eventually(func(g Gomega) {
			g.Expect(apierrors.IsNotFound(actor.Get(ctx, key, &corev1.ConfigMap{}))).To(BeTrue())
			g.Expect(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[0].Name, Name: prefix}, &corev1.ConfigMap{})).To(Succeed())
			_, status := replicationHealthObject(parent)
			g.Expect(status.ProcessedItems).To(HaveLen(1))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		selectNamespace(namespaces[1].Name, true)
		Eventually(func(g Gomega) {
			g.Expect(actor.Get(ctx, key, &corev1.ConfigMap{})).To(Succeed())
			_, status := replicationHealthObject(parent)
			g.Expect(status.ProcessedItems).To(HaveLen(2))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix + "-extra"}, &corev1.ConfigMap{}))).To(BeTrue())
		writer := actor
		if global {
			writer = k8sClient
		}
		By("overlapping full pruning with namespace bootstrap publication")
		selectNamespace(namespaces[0].Name, false)
		for range 2 {
			selectNamespace(namespaces[1].Name, false)
			Eventually(func() error {
				fresh := parent.DeepCopyObject().(client.Object)
				if err := writer.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				annotations := fresh.GetAnnotations()
				if annotations == nil {
					annotations = map[string]string{}
				}
				annotations[meta.ReconcileAnnotation] = time.Now().Format(time.RFC3339Nano)
				fresh.SetAnnotations(annotations)
				return writer.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			selectNamespace(namespaces[1].Name, true)
			Eventually(func(g Gomega) {
				fresh, status := replicationHealthObject(parent)
				g.Expect(fresh.GetAnnotations()[meta.ReconcileAnnotation]).To(BeEmpty())
				g.Expect(status.ProcessedItems).To(HaveLen(1))
				g.Expect(status.ProcessedItems[0].Namespace).To(Equal(namespaces[1].Name))
				g.Expect(fresh.GetFinalizers()).To(ContainElement(meta.ControllerFinalizer))
				g.Expect(actor.Get(ctx, key, &corev1.ConfigMap{})).To(Succeed())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[0].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		By("removing all replicated targets when the parent is deleted")
		Expect(writer.Delete(ctx, parent)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Eventually(func() bool { return apierrors.IsNotFound(actor.Get(ctx, key, &corev1.ConfigMap{})) }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	}, Entry("GlobalTenantResource", true), Entry("TenantResource", false))

	It("uses the current tenant after recreating a namespace under a different owner", Label("bootstrap-review"), func() {
		ctx := context.Background()
		prefix := "bootstrap-owner-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		parents := make([]client.Object, 2)
		for i := range parents {
			name := fmt.Sprintf("%s-%d", prefix, i)
			parent := &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": fmt.Sprint(i) + prefix}},
				TenantResourceCommonSpec: capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{
					NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-profile": "selected"}},
					RawItems:          []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"tenant":%q}}`, name, name))}},
				}}},
			}}
			// Use the controller identity so target RBAC cannot hide a tenant mix-up.
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())
			DeferCleanup(DeleteInBackground, parent)
			parents[i] = parent
			waitWatchReady(parent)
		}
		old := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(namespaces[1]), old)).To(Succeed())
		ForceDeleteNamespace(ctx, old.Name)
		TenantNamespaceReady(&capsulev1beta2.Tenant{Name: prefix + "-0"}, namespaces[0], 1)
		recreated := NewNamespace(old.Name, map[string]string{meta.NewTenantLabel: prefix + "-1", "projectcapsule.dev/watch-profile": "selected"})
		NamespaceCreation(recreated, owners[1], defaultTimeoutInterval).Should(Succeed())
		TenantNamespaceReady(&capsulev1beta2.Tenant{Name: prefix + "-1"}, recreated, 2)
		Eventually(func(g Gomega) {
			fresh := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(recreated), fresh)).To(Succeed())
			g.Expect(fresh.UID).NotTo(Equal(old.UID))
			output := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: recreated.Name, Name: prefix + "-1"}, output)).To(Succeed())
			g.Expect(output.Data).To(HaveKeyWithValue("tenant", prefix+"-1"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Consistently(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: recreated.Name, Name: prefix + "-0"}, &corev1.ConfigMap{}))
		}, 2*time.Second, defaultPollInterval).Should(BeTrue())
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: recreated.Name, Name: prefix + "-1"}, &corev1.ConfigMap{}))).To(BeTrue())
		Eventually(func(g Gomega) {
			for i, parent := range parents {
				fresh, status := replicationHealthObject(parent)
				g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
				g.Expect(status.ProcessedItems).To(HaveLen(1))
				wantNamespace := namespaces[0].Name
				if i == 1 {
					wantNamespace = recreated.Name
				}
				g.Expect(status.ProcessedItems[0].Namespace).To(Equal(wantNamespace))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	DescribeTable("installs selected profiles and retains inventory across new namespaces", func(global, health bool) {
		ctx := context.Background()
		prefix := "bootstrap-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		sourceNamespace := namespaces[0].Name
		grantPolicyAccess := func(namespace string) {
			role := &rbacv1.Role{Name: prefix, Namespace: namespace, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"networking.k8s.io"}, Resources: []string{"networkpolicies"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"}}}}
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			binding := &rbacv1.RoleBinding{Name: prefix, Namespace: namespace, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: prefix}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: "default", Namespace: sourceNamespace}}}
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())
		}
		grantPolicyAccess(sourceNamespace)
		spec := capsulev1beta2.TenantResourceCommonSpec{
			ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour},
			Resources: []capsulev1beta2.ResourceSpec{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-profile": "selected"}},
				Policy:            &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Force: true, Protect: new(true)},
				RawItems:          []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"networking.k8s.io/v1","kind":"NetworkPolicy","metadata":{"name":%q},"spec":{"podSelector":{},"policyTypes":["Ingress","Egress"],"ingress":[],"egress":[]}}`, prefix))}},
			}},
		}
		if health {
			spec.HealthChecks = &capsulev1beta2.TenantResourceHealthChecks{Wait: true}
		}
		parent := createHealthReviewReplication(global, prefix, sourceNamespace, spec, actor)
		waitWatchReady(parent)
		var created []*corev1.Namespace
		for i, selected := range []bool{true, false, true} {
			tenantIndex := 0
			owner := owners[0]
			size := uint(3 + i)
			if i == 2 {
				tenantIndex = 1
				owner = owners[1]
				size = 2
			}
			profile := "excluded"
			if selected {
				profile = "selected"
			}
			namespace := NewNamespace("", map[string]string{meta.NewTenantLabel: fmt.Sprintf("%s-%d", prefix, tenantIndex), "projectcapsule.dev/watch-profile": profile})
			NamespaceCreation(namespace, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, namespace.Name)
			TenantNamespaceReady(&capsulev1beta2.Tenant{Name: fmt.Sprintf("%s-%d", prefix, tenantIndex)}, namespace, size)
			if tenantIndex == 0 {
				grantPolicyAccess(namespace.Name)
			}
			created = append(created, namespace)
		}
		key := client.ObjectKey{Namespace: created[0].Name, Name: prefix}
		Eventually(func(g Gomega) {
			policy := &networkingv1.NetworkPolicy{}
			g.Expect(actor.Get(ctx, key, policy)).To(Succeed())
			g.Expect(policy.Spec.PodSelector.MatchLabels).To(BeEmpty())
			g.Expect(policy.Spec.PolicyTypes).To(ConsistOf(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			fresh, status := replicationHealthObject(parent)
			g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
			g.Expect(status.ProcessedItems).To(HaveLen(2))
			g.Expect(meta.IsStatusConditionTrue(status.Conditions, meta.ReadyCondition)).To(BeTrue())
			for _, item := range status.ProcessedItems {
				g.Expect(item.Namespace).To(BeElementOf(sourceNamespace, created[0].Name))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		for _, namespace := range created[1:] {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix}, &networkingv1.NetworkPolicy{}))).To(BeTrue())
		}
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: created[2].Name, Name: prefix}, &networkingv1.NetworkPolicy{}))).To(BeTrue())
		By("retaining protection against tenant-owner changes after bootstrap")
		Eventually(func() error {
			fresh := &networkingv1.NetworkPolicy{}
			if err := actor.Get(ctx, key, fresh); err != nil {
				return err
			}
			fresh.Spec.Ingress = []networkingv1.NetworkPolicyIngressRule{{}}
			err := actor.Update(ctx, fresh)
			if apierrors.IsConflict(err) {
				return err
			}
			Expect(err).To(HaveOccurred())
			Expect(apierrors.IsForbidden(err)).To(BeTrue(), "%v", err)
			Expect(err).To(MatchError(ContainSubstring("capsule replication")))
			return nil
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		fresh := &networkingv1.NetworkPolicy{}
		Expect(actor.Get(ctx, key, fresh)).To(Succeed())
		Expect(fresh.Spec.Ingress).To(BeEmpty())
	}, Entry("GlobalTenantResource", true, false), Entry("TenantResource", false, false), Entry("GlobalTenantResource with health checks", true, true), Entry("TenantResource with health checks", false, true))

	It("refreshes tenant-dependent generators in existing namespaces after an addition", func() {
		ctx := context.Background()
		prefix := "bootstrap-context-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		spec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespaces[0].Name}},
			Policy:            &apiruntime.ResourceReplicationPolicy{Protect: new(false)},
			Generators:        []capsulev1beta2.TemplateItemSpec{{Template: fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"count":"{{ len .tenant.status.namespaces }}","tenant":"{{ .tenant.metadata.name }}"}}`, prefix)}},
		}}}
		parent := createHealthReviewReplication(true, prefix, namespaces[0].Name, spec, actor)
		waitWatchReady(parent)
		expectCount := func(count int) {
			Eventually(func(g Gomega) {
				output := &corev1.ConfigMap{}
				g.Expect(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[0].Name, Name: prefix}, output)).To(Succeed())
				g.Expect(output.Data).To(HaveKeyWithValue("count", strconv.Itoa(count)))
				g.Expect(output.Data).To(HaveKeyWithValue("tenant", prefix+"-0"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		expectCount(2)
		added := NewNamespace("", map[string]string{meta.NewTenantLabel: prefix + "-0"})
		NamespaceCreation(added, owners[0], defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, added.Name)
		TenantNamespaceReady(&capsulev1beta2.Tenant{Name: prefix + "-0"}, added, 3)
		expectCount(3)
		for _, namespace := range []*corev1.Namespace{added, namespaces[2]} {
			Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace.Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		}
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("continues bootstrapping when another source fails rendering", func() {
		ctx := context.Background()
		prefix := "bootstrap-failure-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		brokenSpec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{Generators: []capsulev1beta2.TemplateItemSpec{{Template: `not-a-kubernetes-object`}}}}}
		broken := createHealthReviewReplication(true, prefix, namespaces[0].Name, brokenSpec, actor)
		Eventually(func(g Gomega) {
			_, status := replicationHealthObject(broken)
			condition := status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			if condition != nil {
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Message).To(ContainSubstring("decode yaml"))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		workingName := prefix + "-working"
		workingSpec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"tenant":"{{tenant.name}}"}}`, workingName))}}}}}
		working := createHealthReviewReplication(false, workingName, namespaces[0].Name, workingSpec, actor)
		// The local source targets the initial second namespace as well.
		bindServiceAccountToTenantResourceManager(namespaces[0].Name, "default", namespaces[1].Name)
		waitWatchReady(working)
		for i := range 2 {
			added := NewNamespace("", map[string]string{meta.NewTenantLabel: prefix + "-0"})
			NamespaceCreation(added, owners[0], defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(DeleteNamespaceInBackground, ctx, added.Name)
			bindServiceAccountToTenantResourceManager(namespaces[0].Name, "default", added.Name)
			TenantNamespaceReady(&capsulev1beta2.Tenant{Name: prefix + "-0"}, added, uint(3+i))
			Eventually(func(g Gomega) {
				output := &corev1.ConfigMap{}
				g.Expect(actor.Get(ctx, client.ObjectKey{Namespace: added.Name, Name: workingName}, output)).To(Succeed())
				g.Expect(output.Data).To(HaveKeyWithValue("tenant", prefix+"-0"))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: workingName}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: workingName}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	It("repairs drift after verified no-op resyncs without taking unrelated fields", func() {
		ctx := context.Background()
		prefix := "replication-noop-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		target := &corev1.ConfigMap{Name: prefix, Namespace: namespaces[0].Name, Data: map[string]string{"managed": "old", "unmanaged": "retained"}}
		Expect(actor.Create(ctx, target)).To(Succeed())
		uid := target.UID
		spec := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": target.Namespace}}, Policy: &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Force: true, Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q},"data":{"managed":"desired"}}`, prefix))}}}}}
		parent := createHealthReviewReplication(false, prefix, target.Namespace, spec, actor)
		waitWatchReady(parent)
		reconcile := func() {
			Eventually(func() error {
				fresh := &capsulev1beta2.TenantResource{}
				if err := actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				if fresh.Annotations == nil {
					fresh.Annotations = map[string]string{}
				}
				fresh.Annotations[meta.ReconcileAnnotation] = time.Now().Format(time.RFC3339Nano)
				return actor.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				fresh := &capsulev1beta2.TenantResource{}
				g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(parent), fresh)).To(Succeed())
				g.Expect(fresh.Annotations[meta.ReconcileAnnotation]).To(BeEmpty())
				g.Expect(meta.IsStatusConditionTrue(fresh.Status.Conditions, meta.ReadyCondition)).To(BeTrue())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		reconcile()
		reconcile()
		Eventually(func() error {
			fresh := &corev1.ConfigMap{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(target), fresh); err != nil {
				return err
			}
			fresh.Data["managed"] = "drift"
			return actor.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		reconcile()
		Eventually(func(g Gomega) {
			fresh := &corev1.ConfigMap{}
			g.Expect(actor.Get(ctx, client.ObjectKeyFromObject(target), fresh)).To(Succeed())
			g.Expect(fresh.UID).To(Equal(uid))
			g.Expect(fresh.Data).To(Equal(map[string]string{"managed": "desired", "unmanaged": "retained"}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	})

	DescribeTable("rejects an invalid health revision before bootstrapping", func(global bool) {
		ctx := context.Background()
		prefix := "bootstrap-validation-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		sourceNamespace := namespaces[0].Name
		spec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-profile": "selected"}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}
		parent := createHealthReviewReplication(global, prefix, sourceNamespace, spec, actor)
		waitWatchReady(parent)
		writer := actor
		if global {
			writer = k8sClient
		}
		setExpression := func(expression string) {
			Eventually(func() error {
				fresh := parent.DeepCopyObject().(client.Object)
				if err := writer.Get(ctx, client.ObjectKeyFromObject(parent), fresh); err != nil {
					return err
				}
				checks := &capsulev1beta2.TenantResourceHealthChecks{Wait: true, Expressions: []capsulev1beta2.TenantResourceHealthCheckExpression{{APIVersion: "v1", Kind: "ConfigMap", Current: expression}}}
				switch source := fresh.(type) {
				case *capsulev1beta2.GlobalTenantResource:
					source.Spec.HealthChecks = checks
				case *capsulev1beta2.TenantResource:
					source.Spec.HealthChecks = checks
				}
				return writer.Update(ctx, fresh)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		setExpression("object.(")
		Eventually(func(g Gomega) {
			fresh, status := replicationHealthObject(parent)
			g.Expect(status.ObservedGeneration).To(Equal(fresh.GetGeneration()))
			condition := status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Message).To(ContainSubstring("healthChecks.expressions[0]"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		added := NewNamespace("", map[string]string{meta.NewTenantLabel: prefix + "-0", "projectcapsule.dev/watch-profile": "selected"})
		NamespaceCreation(added, owners[0], defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, added.Name)
		bindServiceAccountToTenantResourceManager(sourceNamespace, "default", added.Name)
		TenantNamespaceReady(&capsulev1beta2.Tenant{Name: prefix + "-0"}, added, 3)
		key := client.ObjectKey{Namespace: added.Name, Name: prefix}
		Consistently(func() bool { return apierrors.IsNotFound(actor.Get(ctx, key, &corev1.ConfigMap{})) }, 2*time.Second, defaultPollInterval).Should(BeTrue(), "invalid health checks must prevent scoped writes")
		setExpression("true")
		waitWatchReady(parent)
		Expect(actor.Get(ctx, key, &corev1.ConfigMap{})).To(Succeed())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("GlobalTenantResource", true), Entry("TenantResource", false))

	DescribeTable("waits for dependencies before bootstrapping a new namespace", func(global bool) {
		ctx := context.Background()
		prefix := "bootstrap-dependency-" + rand.String(5)
		owners, namespaces := watchNamespaces(prefix)
		actor := impersonationClient(owners[0].Name, withDefaultGroups(nil))
		sourceNamespace := namespaces[0].Name
		dependencyName := prefix + "-dependency"
		dependencySpec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": sourceNamespace}}, Generators: []capsulev1beta2.TemplateItemSpec{{Template: `not-a-kubernetes-object`}}}}}
		var dependency client.Object = &capsulev1beta2.TenantResource{Name: dependencyName, Namespace: sourceNamespace, Spec: capsulev1beta2.TenantResourceSpec{ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: "default"}, TenantResourceCommonSpec: dependencySpec}}
		if global {
			dependency = &capsulev1beta2.GlobalTenantResource{Name: dependencyName, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-tenant": "0" + prefix}}, ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: "default", Namespace: meta.RFC1123SubdomainName(sourceNamespace)}, TenantResourceCommonSpec: dependencySpec}}
		}
		Expect(k8sClient.Create(ctx, dependency)).To(Succeed())
		DeferCleanup(DeleteInBackground, dependency)
		spec := capsulev1beta2.TenantResourceCommonSpec{HealthChecks: &capsulev1beta2.TenantResourceHealthChecks{Wait: true}, ResyncPeriod: metav1.Duration{Duration: 24 * time.Hour}, DependsOn: []meta.LocalRFC1123ObjectReference{{Name: meta.RFC1123Name(dependencyName)}}, Resources: []capsulev1beta2.ResourceSpec{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"projectcapsule.dev/watch-profile": "selected"}}, Policy: &apiruntime.ResourceReplicationPolicy{Protect: new(false)}, RawItems: []capsulev1beta2.RawExtension{{Raw: []byte(fmt.Sprintf(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":%q}}`, prefix))}}}}}
		parent := createHealthReviewReplication(global, prefix, sourceNamespace, spec, actor)
		Eventually(func(g Gomega) {
			_, status := replicationHealthObject(parent)
			condition := status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(condition).NotTo(BeNil())
			if condition != nil {
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Message).To(ContainSubstring("not ready"))
			}
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		added := NewNamespace("", map[string]string{meta.NewTenantLabel: prefix + "-0", "projectcapsule.dev/watch-profile": "selected"})
		NamespaceCreation(added, owners[0], defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(DeleteNamespaceInBackground, ctx, added.Name)
		bindServiceAccountToTenantResourceManager(sourceNamespace, "default", added.Name)
		TenantNamespaceReady(&capsulev1beta2.Tenant{Name: prefix + "-0"}, added, 3)
		key := client.ObjectKey{Namespace: added.Name, Name: prefix}
		Consistently(func() bool { return apierrors.IsNotFound(actor.Get(ctx, key, &corev1.ConfigMap{})) }, 2*time.Second, defaultPollInterval).Should(BeTrue(), "bootstrap must not apply before the dependency is ready")
		Eventually(func() error {
			fresh := dependency.DeepCopyObject().(client.Object)
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dependency), fresh); err != nil {
				return err
			}
			switch source := fresh.(type) {
			case *capsulev1beta2.GlobalTenantResource:
				source.Spec.Resources[0].Generators[0].Template = ""
			case *capsulev1beta2.TenantResource:
				source.Spec.Resources[0].Generators[0].Template = ""
			}
			return k8sClient.Update(ctx, fresh)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func() error { return actor.Get(ctx, key, &corev1.ConfigMap{}) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		waitWatchReady(parent)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
		Expect(apierrors.IsForbidden(actor.Get(ctx, client.ObjectKey{Namespace: namespaces[2].Name, Name: prefix}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("GlobalTenantResource", true), Entry("TenantResource", false))
})
