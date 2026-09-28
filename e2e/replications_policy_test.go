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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/ssa"
)

// Shared scenarios run inside both existing replication suites and their tenant fixtures.
func exerciseReplicationPolicies(global bool, tenantName, baseNamespace, targetNamespace, excludedNamespace string, owner rbac.UserSpec) {
	ctx := context.Background()
	selected := []string{targetNamespace}
	if global {
		// This namespace matches the block selector but belongs to an unselected tenant.
		selected = append(selected, excludedNamespace)
	}
	selector := &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpIn, Values: selected}}}
	block := func(name string, policy *apiruntime.ResourceReplicationPolicy) capsulev1beta2.ResourceSpec {
		return capsulev1beta2.ResourceSpec{
			NamespaceSelector: selector,
			Policy:            policy,
			RawItems: []capsulev1beta2.RawExtension{{Object: &corev1.ConfigMap{
				APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"mode": "replicated"},
			}}},
		}
	}
	common := capsulev1beta2.TenantResourceCommonSpec{
		ResyncPeriod:    resyncPeriod,
		Settings:        capsulev1beta2.TenantResourceCommonSpecSettings{Adopt: new(true), Force: new(true)},
		PruningOnDelete: new(false),
		Resources: []capsulev1beta2.ResourceSpec{
			block("policy-legacy", nil),
			block("policy-owner", &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyOwner, Protect: new(false)}),
			block("policy-conflict", &apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge}),
			block("policy-unprotected", &apiruntime.ResourceReplicationPolicy{Protect: new(false)}),
		},
	}
	var parent client.Object
	var spec *capsulev1beta2.TenantResourceCommonSpec
	var status *capsulev1beta2.TenantResourceCommonStatus
	if global {
		resource := &capsulev1beta2.GlobalTenantResource{Name: "gtr-block-policies", Spec: capsulev1beta2.GlobalTenantResourceSpec{
			Scope:          api.ResourceScopeNamespace,
			TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"energy": "solar"}}, TenantResourceCommonSpec: common,
		}}
		parent, spec, status = resource, &resource.Spec.TenantResourceCommonSpec, &resource.Status.TenantResourceCommonStatus
	} else {
		resource := &capsulev1beta2.TenantResource{Name: "tr-block-policies", Namespace: baseNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
		parent, spec, status = resource, &resource.Spec.TenantResourceCommonSpec, &resource.Status.TenantResourceCommonStatus
	}
	parentKey := client.ObjectKeyFromObject(parent)
	By("seeding externally owned objects before creating the replication")
	for _, name := range []string{"policy-owner", "policy-conflict"} {
		seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Namespace: targetNamespace, Data: map[string]string{"outside": "retained"}}
		if name == "policy-conflict" {
			seed.Data["mode"] = "external"
		}
		Expect(k8sClient.Patch(ctx, seed, client.Apply, client.FieldOwner("e2e-policy-external"))).To(Succeed())
	}
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{Name: "policy-conflict", Namespace: excludedNamespace, Data: map[string]string{"mode": "excluded"}})).To(Succeed())
	Expect(k8sClient.Create(ctx, parent)).To(Succeed())
	By("retaining deprecated fields while converting only the missing policy")
	Expect(spec.Settings.Adopt).To(Equal(new(true)))
	Expect(spec.Settings.Force).To(Equal(new(true)))
	Expect(spec.Resources[0].Policy).To(Equal(&apiruntime.ResourceReplicationPolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Force: true, Protect: new(true), Deletion: apiruntime.ResourceDeletionPolicyOrphan}))
	Expect(spec.Resources[1].Policy.Creation).To(Equal(apiruntime.ResourceCreationPolicyOwner))
	Expect(spec.Resources[2].Policy.Force).To(BeFalse())
	By("rejecting adoption and SSA conflicts according to each explicit block policy")
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, parentKey, parent)).To(Succeed())
		for name, message := range map[string]string{"policy-owner": "cannot be adopted", "policy-conflict": "conflict"} {
			found := false
			for _, item := range status.ProcessedItems {
				if item.Name == name && item.Namespace == targetNamespace {
					found = true
					g.Expect(item.Tenant).To(Equal(tenantName))
					g.Expect(item.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(item.Message).To(ContainSubstring(message))
				}
			}
			g.Expect(found).To(BeTrue(), name)
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	By("enabling adoption and force per block and converting a new block on update")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, parentKey, parent); err != nil {
			return err
		}
		spec.Resources[1].Policy.Creation = apiruntime.ResourceCreationPolicyMerge
		spec.Resources[2].Policy.Force = true
		if len(spec.Resources) == 4 {
			spec.Resources = append(spec.Resources, block("policy-update", nil))
		}
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(spec.Resources[4].Policy).To(Equal(spec.Resources[0].Policy))
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, parentKey, parent)).To(Succeed())
		g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		g.Expect(status.ProcessedItems).To(HaveLen(5))
		for _, item := range status.ProcessedItems {
			g.Expect(item.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(item.LastApply.IsZero()).To(BeFalse())
			g.Expect(item.Policy).NotTo(BeNil())
			g.Expect(item.Namespace).To(Equal(targetNamespace))
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	expectConfigMapData(targetNamespace, "policy-conflict", map[string]string{"mode": "replicated", "outside": "retained"})
	expectConfigMapData(excludedNamespace, "policy-conflict", map[string]string{"mode": "excluded"})
	expectConfigMapAbsent(excludedNamespace, "policy-legacy")
	ownerClient := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
	By("protecting created and adopted targets while permitting explicitly unprotected targets")
	for _, name := range []string{"policy-legacy", "policy-conflict"} {
		Eventually(func(g Gomega) {
			cm := &corev1.ConfigMap{}
			g.Expect(ownerClient.Get(ctx, client.ObjectKey{Namespace: targetNamespace, Name: name}, cm)).To(Succeed())
			err := ownerClient.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"e2e-policy":"denied"}}}`)))
			g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), fmt.Sprint(err))
			g.Expect(apierrors.IsForbidden(ownerClient.Delete(ctx, cm))).To(BeTrue())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	for _, name := range []string{"policy-owner", "policy-unprotected"} {
		cm := &corev1.ConfigMap{Name: name, Namespace: targetNamespace}
		Expect(ownerClient.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"e2e-policy":"allowed"}}}`)))).To(Succeed())
	}
	By("updating policies on created and adopted targets while their content is skipped")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, parentKey, parent); err != nil {
			return err
		}
		for i := 1; i < len(spec.Resources); i++ {
			spec.Resources[i].Policy.Condition = "false"
		}
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, parentKey, parent)).To(Succeed())
		g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		for _, item := range status.ProcessedItems {
			if item.Name != "policy-legacy" {
				g.Expect(item.Message).To(Equal(ssa.ConditionNotMet))
			}
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	lastApply := map[string]metav1.Time{}
	for _, item := range status.ProcessedItems {
		lastApply[item.Name] = item.LastApply
	}
	Eventually(func() error {
		if err := k8sClient.Get(ctx, parentKey, parent); err != nil {
			return err
		}
		for i := 1; i < len(spec.Resources); i++ {
			spec.Resources[i].Policy.Condition = "false"
			spec.Resources[i].RawItems[0] = capsulev1beta2.RawExtension{Object: &corev1.ConfigMap{
				APIVersion: "v1", Kind: "ConfigMap", Name: []string{"", "policy-owner", "policy-conflict", "policy-unprotected", "policy-update"}[i],
				Data: map[string]string{"mode": "must-not-apply"},
			}}
		}
		spec.Resources[1].Policy.Protect = new(true)
		spec.Resources[2].Policy.Protect = new(false)
		spec.Resources[2].Policy.Force = false
		spec.Resources[2].Policy.Creation = apiruntime.ResourceCreationPolicyOwner
		spec.Resources[3].Policy.Protect = new(true)
		spec.Resources[3].Policy.Deletion = apiruntime.ResourceDeletionPolicyOrphan
		spec.Resources[4].Policy.Protect = new(false)
		spec.Resources[4].Policy.Deletion = apiruntime.ResourceDeletionPolicyRemove
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, parentKey, parent)).To(Succeed())
		g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		for _, item := range status.ProcessedItems {
			if item.Name == "policy-legacy" {
				continue
			}
			g.Expect(item.Message).To(Equal(ssa.ConditionNotMet))
			previous := lastApply[item.Name]
			g.Expect(item.LastApply.Equal(&previous)).To(BeTrue(), "content apply timestamp changed for "+item.Name)
			protected := item.Name == "policy-owner" || item.Name == "policy-unprotected"
			g.Expect(item.Policy.IsProtected()).To(Equal(protected))
			created := item.Name == "policy-unprotected" || item.Name == "policy-update"
			g.Expect(item.Created).To(Equal(created), "creation policy changed the target's lifecycle origin")
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: targetNamespace, Name: item.Name}, cm)).To(Succeed())
			g.Expect(cm.Data).To(HaveKeyWithValue("mode", "replicated"))
			g.Expect(cm.Labels[meta.ProtectedByCapsuleLabel] == meta.ValueControllerReplications).To(Equal(protected))
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	By("enforcing changed protection without applying the skipped content")
	for _, name := range []string{"policy-owner", "policy-unprotected"} {
		Eventually(func(g Gomega) {
			cm := &corev1.ConfigMap{Name: name, Namespace: targetNamespace}
			err := ownerClient.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"policy-after-skip":"denied"}}}`)))
			g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), fmt.Sprint(err))
			g.Expect(apierrors.IsForbidden(ownerClient.Delete(ctx, cm))).To(BeTrue())
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	for _, name := range []string{"policy-conflict", "policy-update"} {
		Eventually(func() error {
			return ownerClient.Patch(ctx, &corev1.ConfigMap{Name: name, Namespace: targetNamespace}, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"policy-after-skip":"allowed"}}}`)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	expectConfigMapData(excludedNamespace, "policy-conflict", map[string]string{"mode": "excluded"})
	By("removing lifecycle-owned protection when conditional content applies again")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, parentKey, parent); err != nil {
			return err
		}
		for _, i := range []int{1, 3} {
			spec.Resources[i].Policy.Condition = "true"
			spec.Resources[i].Policy.Protect = new(false)
			name := "policy-owner"
			if i == 3 {
				name = "policy-unprotected"
			}
			spec.Resources[i].RawItems[0] = capsulev1beta2.RawExtension{Object: &corev1.ConfigMap{
				APIVersion: "v1", Kind: "ConfigMap", Name: name,
				Data: map[string]string{"mode": "replicated", "resumed": "true"},
			}}
		}
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, parentKey, parent)).To(Succeed())
		g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		for _, name := range []string{"policy-owner", "policy-unprotected"} {
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: targetNamespace, Name: name}, cm)).To(Succeed())
			g.Expect(cm.Data).To(HaveKeyWithValue("resumed", "true"))
			g.Expect(cm.Labels).NotTo(HaveKey(meta.ProtectedByCapsuleLabel))
			g.Expect(ownerClient.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"policy-resumed":"allowed"}}}`)))).To(Succeed())
			g.Expect(ownerClient.Delete(ctx, cm, client.DryRunAll)).To(Succeed())
		}
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	By("orphaning removed items with the reconciled policy")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, parentKey, parent); err != nil {
			return err
		}
		spec.Resources[0].RawItems = nil
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	expectReplicationPolicyOrphan(targetNamespace, "policy-legacy")
	Expect(ownerClient.Delete(ctx, &corev1.ConfigMap{Name: "policy-legacy", Namespace: targetNamespace})).To(Succeed())
	By("applying Remove and Orphan independently when the parent is deleted")
	Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
	Eventually(func() bool { return apierrors.IsNotFound(k8sClient.Get(ctx, parentKey, parent)) }, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	expectReplicationPolicyOrphan(targetNamespace, "policy-unprotected")
	expectConfigMapAbsent(targetNamespace, "policy-update")
	for _, name := range []string{"policy-owner", "policy-conflict"} {
		Eventually(func(g Gomega) {
			cm := &corev1.ConfigMap{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: targetNamespace}, cm)).To(Succeed())
			g.Expect(cm.Data).To(Equal(map[string]string{"outside": "retained"}))
			g.Expect(cm.Labels).NotTo(HaveKey(meta.NewManagedByCapsuleLabel))
			g.Expect(cm.Labels).NotTo(HaveKey(meta.ProtectedByCapsuleLabel))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
}

func expectReplicationPolicyOrphan(namespace, name string) {
	Eventually(func(g Gomega) {
		cm := &corev1.ConfigMap{}
		g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, cm)).To(Succeed())
		g.Expect(cm.Data).To(HaveKeyWithValue("mode", "replicated"))
		for _, label := range []string{meta.CreatedByCapsuleLabel, meta.NewManagedByCapsuleLabel, meta.ProtectedByCapsuleLabel} {
			g.Expect(cm.Labels).NotTo(HaveKey(label))
		}
		g.Expect(cm.OwnerReferences).To(BeEmpty())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
}
