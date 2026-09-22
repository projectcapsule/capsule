// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

func exerciseSharedReplicationProtection(global bool, tenantName, baseNamespace, targetNamespace, excludedNamespace string, owner rbac.UserSpec, orphan bool) {
	ctx := context.Background()
	const name = "shared-protected"
	seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Namespace: targetNamespace, Data: map[string]string{"outside": "retained"}}
	Expect(k8sClient.Patch(ctx, seed, client.Apply, client.FieldOwner("e2e-external"))).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{Name: name, Namespace: excludedNamespace, Data: map[string]string{"outside": "excluded"}})).To(Succeed())
	selected := []string{targetNamespace}
	if global {
		selected = append(selected, excludedNamespace)
	}
	type replication struct {
		object client.Object
		spec   *capsulev1beta2.TenantResourceCommonSpec
		status *capsulev1beta2.TenantResourceCommonStatus
	}
	parents := make([]replication, 2)
	By("adopting one target through two independent resource blocks")
	for i := range parents {
		common := capsulev1beta2.TenantResourceCommonSpec{
			ResyncPeriod: resyncPeriod,
			Resources: []capsulev1beta2.ResourceSpec{{
				NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpIn, Values: selected}}},
				Policy: &apiruntime.ResourceTemplatePolicy{
					Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyRemove,
				},
				RawItems: []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"shared": "managed"}}}},
			}},
		}
		parentName := fmt.Sprintf("shared-protection-%d", i)
		if global {
			obj := &capsulev1beta2.GlobalTenantResource{Name: parentName, Spec: capsulev1beta2.GlobalTenantResourceSpec{
				Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"energy": "solar"}}, TenantResourceCommonSpec: common,
			}}
			parents[i] = replication{obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus}
		} else {
			obj := &capsulev1beta2.TenantResource{Name: parentName, Namespace: baseNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
			parents[i] = replication{obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus}
		}
		parent := parents[i]
		Expect(k8sClient.Create(ctx, parent.object)).To(Succeed())
		DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, parent.object)) })
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent.object), parent.object)).To(Succeed())
			g.Expect(parent.status.ProcessedItems).To(HaveLen(1))
			item := parent.status.ProcessedItems[0]
			g.Expect(item.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(item.LastApply.IsZero()).To(BeFalse(), "both managers must have completed an apply before testing cleanup")
			g.Expect(parent.object.GetFinalizers()).To(ContainElement(meta.ControllerFinalizer))
			g.Expect(item.Created).To(BeFalse())
			g.Expect(item.Tenant).To(Equal(tenantName))
			g.Expect(item.Namespace).To(Equal(targetNamespace))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	By("assigning protection through skipped policy reconciliation")
	for i, parent := range parents {
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent.object), parent.object); err != nil {
				return err
			}
			parent.spec.Resources[0].Policy.Condition = "false"
			parent.spec.Resources[0].Policy.Protect = new(true)
			if orphan && i == 0 {
				parent.spec.Resources[0].Policy.Deletion = apiruntime.ResourceDeletionPolicyOrphan
			}
			return k8sClient.Update(ctx, parent.object)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent.object), parent.object)).To(Succeed())
			g.Expect(parent.status.ObservedGeneration).To(Equal(parent.object.GetGeneration()))
			g.Expect(parent.status.ProcessedItems).To(HaveLen(1))
			g.Expect(parent.status.ProcessedItems[0].Message).To(Equal(ssa.ConditionNotMet))
			g.Expect(parent.status.ProcessedItems[0].Policy.IsProtected()).To(BeTrue())
			g.Expect(parent.status.ProcessedItems[0].Policy.ShouldOrphan()).To(Equal(orphan && i == 0))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	By("cordoning the surviving parent so it cannot repair incorrect cleanup")
	survivor := parents[1]
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(survivor.object), survivor.object); err != nil {
			return err
		}
		survivor.spec.Cordoned = new(true)
		return k8sClient.Update(ctx, survivor.object)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(survivor.object), survivor.object)).To(Succeed())
		condition := survivor.status.Conditions.GetConditionByType(meta.CordonedCondition)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	if !orphan {
		By("matching legacy and independent markers without controller repair")
		first := parents[0]
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(first.object), first.object); err != nil {
				return err
			}
			first.spec.Cordoned = new(true)
			return k8sClient.Update(ctx, first.object)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(first.object), first.object)).To(Succeed())
			condition := first.status.Conditions.GetConditionByType(meta.CordonedCondition)
			g.Expect(condition).NotTo(BeNil())
			g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNamespace}, cm)).To(Succeed())
		original := maps.Clone(cm.Labels)
		sa := survivor.status.ServiceAccount
		Expect(sa).NotTo(BeNil())
		runner := impersonationClient(serviceAccountUsername(sa.Namespace.String(), sa.Name.String()), serviceAccountGroups(sa.Namespace.String()))
		ownerClient := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
		for _, marker := range []map[string]string{
			{meta.CreatedByCapsuleLabel: meta.ValueControllerReplications},
			{meta.NewManagedByCapsuleLabel: meta.ValueControllerReplications},
			{meta.ReplicationProtectionLabel: meta.ValueTrue},
			original,
		} {
			// Tenant ownership labels are maintained by metadata admission.
			// Vary only the markers involved in protection webhook selection.
			expected := maps.Clone(original)
			for _, key := range []string{meta.CreatedByCapsuleLabel, meta.NewManagedByCapsuleLabel, meta.ProtectedByCapsuleLabel, meta.ReplicationProtectionLabel, meta.ResourcePermitProtectionLabel} {
				delete(expected, key)
			}
			maps.Copy(expected, marker)
			patch, err := json.Marshal([]map[string]any{{"op": "replace", "path": "/metadata/labels", "value": expected}})
			Expect(err).NotTo(HaveOccurred())
			Expect(runner.Patch(ctx, cm, client.RawPatch(types.JSONPatchType, patch))).To(Succeed())
			err = ownerClient.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":null}}`)))
			Expect(err).To(MatchError(ContainSubstring("is managed by a")))
			Expect(ownerClient.Delete(ctx, cm, client.DryRunAll)).To(MatchError(ContainSubstring("is managed by a")))
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
			Expect(cm.Labels).To(Equal(expected))
		}
	}
	removeParent := func(parent replication) {
		Expect(k8sClient.Delete(ctx, parent.object)).To(Succeed())
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent.object), parent.object))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	}
	By("preserving shared tracking and denying owner writes after the first parent leaves")
	removeParent(parents[0])
	actor := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
	cm := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNamespace}, cm)).To(Succeed())
	Expect(cm.Data).To(Equal(map[string]string{"outside": "retained", "shared": "managed"}))
	Expect(cm.Labels).To(HaveKeyWithValue(meta.NewManagedByCapsuleLabel, meta.ValueControllerReplications))
	Expect(cm.Labels).To(HaveKeyWithValue(meta.ProtectedByCapsuleLabel, meta.ValueControllerReplications))
	err := actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"shared-protection":"denied"}}}`)))
	Expect(apierrors.IsForbidden(err)).To(BeTrue(), fmt.Sprint(err))
	Expect(err.Error()).To(ContainSubstring("is managed by a"))
	err = actor.Delete(ctx, cm, client.DryRunAll)
	Expect(apierrors.IsForbidden(err)).To(BeTrue(), fmt.Sprint(err))
	Expect(err.Error()).To(ContainSubstring("is managed by a"))
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
	Expect(cm.Annotations).NotTo(HaveKey("shared-protection"))
	expectConfigMapData(excludedNamespace, name, map[string]string{"outside": "excluded"})
	By("allowing owner writes after the final active parent leaves and retaining orphaned fields")
	removeParent(survivor)
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
	if orphan {
		// Orphan retains the departing manager's fields and SSA ownership.
		Expect(cm.Data).To(Equal(map[string]string{"outside": "retained", "shared": "managed"}))
	} else {
		Expect(cm.Data).To(Equal(map[string]string{"outside": "retained"}))
		Expect(cm.Labels).NotTo(HaveKey(meta.NewManagedByCapsuleLabel))
		Expect(cm.Labels).NotTo(HaveKey(meta.ProtectedByCapsuleLabel))
	}
	Eventually(func() error {
		return actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"shared-protection":"allowed"}}}`)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(actor.Delete(ctx, cm)).To(Succeed())
}
