// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
)

var _ = Describe("Replication target identity", Label("replications", "protection-identity"), func() {
	DescribeTable("reserves markers and rejects a stale manager of a replacement target", func(global bool) {
		ctx := context.Background()
		prefix := "e2e-replica-identity-" + rand.String(6)
		var tenants []*capsulev1beta2.Tenant
		var actors []client.Client
		By("rejecting forged markers while allowing ordinary owner writes in two tenants")
		for _, suffix := range []string{"-a", "-b"} {
			name := prefix + suffix
			owner := rbac.UserSpec{Name: name + "-owner", Kind: rbac.UserOwner}
			tenant := &capsulev1beta2.Tenant{Name: name, Labels: map[string]string{"env": "e2e", "identity-test": name}, Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{UserSpec: owner}}}}
			Expect(k8sClient.Create(ctx, tenant)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tenant) })
			TenantReady(tenant, metav1.ConditionTrue, defaultTimeoutInterval)
			ns := NewNamespace(name, map[string]string{meta.TenantLabel: name})
			NamespaceCreation(ns, owner, defaultTimeoutInterval).Should(Succeed())
			DeferCleanup(func() { ForceDeleteNamespace(ctx, name) })
			TenantNamespaceReady(tenant, ns, 1)
			tenants = append(tenants, tenant)
			actor := impersonationClient(owner.Name, withDefaultGroups(nil))
			actors = append(actors, actor)
			cm := &corev1.ConfigMap{Name: "ordinary", Namespace: name, Data: map[string]string{"tenant": name}}
			Expect(actor.Create(ctx, cm)).To(Succeed())
			for marker, value := range map[string]string{
				meta.ReplicationProtectionLabel: meta.ValueTrue,
				meta.ProtectedByCapsuleLabel:    meta.ValueControllerReplications,
				meta.CreatedByCapsuleLabel:      meta.ValueControllerReplications,
				meta.NewManagedByCapsuleLabel:   meta.ValueControllerReplications,
			} {
				forged := &corev1.ConfigMap{Name: "forged", Namespace: name, Labels: map[string]string{marker: value}}
				Expect(actor.Create(ctx, forged)).To(MatchError(ContainSubstring("replication metadata")))
				Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(forged), &corev1.ConfigMap{}))).To(BeTrue())
				patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": forged.Labels}})
				Expect(err).NotTo(HaveOccurred())
				Expect(actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, patch))).To(MatchError(ContainSubstring("replication metadata")))
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				Expect(cm.Labels).NotTo(HaveKey(marker))
			}
			Expect(actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"owner-write":"allowed"}}}`)))).To(Succeed())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
			Expect(cm.Annotations).To(HaveKeyWithValue("owner-write", "allowed"))
		}
		namespace := tenants[0].Name
		var oldUID types.UID
		var runners []client.Client
		for i := range 2 {
			By(fmt.Sprintf("applying target incarnation %d with its own execution identity", i))
			name := fmt.Sprintf("%s-parent-%d", prefix, i)
			ensureServiceAccount(namespace, name)
			bindServiceAccountToTenantResourceManager(namespace, name, namespace)
			runner := impersonationClient(serviceAccountUsername(namespace, name), serviceAccountGroups(namespace))
			runners = append(runners, runner)
			common := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: resyncPeriod, Resources: []capsulev1beta2.ResourceSpec{{
				NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
				Policy:            &apiruntime.ResourceReplicationPolicy{Protect: new(true)},
				RawItems:          []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: "replacement", Data: map[string]string{"manager": name}}}},
			}}}
			var parent client.Object
			var status *capsulev1beta2.TenantResourceCommonStatus
			var spec *capsulev1beta2.TenantResourceCommonSpec
			if global {
				obj := &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{
					Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"identity-test": namespace}},
					ServiceAccount: &meta.NamespacedRFC1123ObjectReferenceWithNamespace{Name: meta.RFC1123Name(name), Namespace: meta.RFC1123SubdomainName(namespace)}, TenantResourceCommonSpec: common,
				}}
				parent, status, spec = obj, &obj.Status.TenantResourceCommonStatus, &obj.Spec.TenantResourceCommonSpec
			} else {
				obj := &capsulev1beta2.TenantResource{Name: name, Namespace: namespace, Spec: capsulev1beta2.TenantResourceSpec{
					ServiceAccount: &meta.LocalRFC1123ObjectReference{Name: meta.RFC1123Name(name)}, TenantResourceCommonSpec: common,
				}}
				parent, status, spec = obj, &obj.Status.TenantResourceCommonStatus, &obj.Spec.TenantResourceCommonSpec
			}
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(parent) })
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
				g.Expect(status.ProcessedItems).To(HaveLen(1))
				g.Expect(status.ProcessedItems[0].Status).To(Equal(metav1.ConditionTrue))
				g.Expect(status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func() error {
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
					return err
				}
				spec.Cordoned = new(true)
				return k8sClient.Update(ctx, parent)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
				condition := status.Conditions.GetConditionByType(meta.CordonedCondition)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "replacement"}, cm)).To(Succeed())
			if i == 0 {
				oldUID = cm.UID
				Expect(runner.Delete(ctx, cm)).To(Succeed())
			} else {
				Expect(cm.UID).NotTo(Equal(oldUID))
				By("denying the stale execution identity and tenant owner while allowing the current manager")
				patch := client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"current-manager":"allowed"}}}`))
				for _, denied := range []client.Client{runners[0], actors[0]} {
					Expect(denied.Patch(ctx, cm, patch)).To(MatchError(ContainSubstring("is managed by a")))
					Expect(denied.Delete(ctx, cm, client.DryRunAll)).To(MatchError(ContainSubstring("is managed by a")))
				}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				Expect(cm.Annotations).NotTo(HaveKey("current-manager"))
				Expect(runner.Patch(ctx, cm, patch)).To(Succeed())
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				Expect(cm.Annotations).To(HaveKeyWithValue("current-manager", "allowed"))
				Expect(cm.Data).To(Equal(map[string]string{"manager": name}))
			}
		}
		expectConfigMapData(tenants[1].Name, "ordinary", map[string]string{"tenant": tenants[1].Name})
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Namespace: tenants[1].Name, Name: "replacement"}, &corev1.ConfigMap{}))).To(BeTrue())
	}, Entry("TenantResource", false), Entry("GlobalTenantResource", true))
})
