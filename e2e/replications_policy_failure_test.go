// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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

func exerciseReplicationPolicyFailure(global bool, tenantName, baseNamespace, targetNamespace, excludedNamespace string, owner rbac.UserSpec) {
	ctx := context.Background()
	name := fmt.Sprintf("policy-failure-%t", global)
	key := client.ObjectKey{Name: name, Namespace: targetNamespace}
	target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"managed": "initial"}}
	seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Namespace: targetNamespace, Data: map[string]string{"external": "retained"}}
	Expect(k8sClient.Patch(ctx, seed, client.Apply, client.FieldOwner("e2e-external"))).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{Name: name, Namespace: excludedNamespace, Data: map[string]string{"external": "excluded"}})).To(Succeed())
	common := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: resyncPeriod, Resources: []capsulev1beta2.ResourceSpec{{
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNamespace}},
		Policy:            &apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(false), Deletion: apiruntime.ResourceDeletionPolicyOrphan},
		RawItems:          []capsulev1beta2.RawExtension{{Object: target}},
	}}}
	var parent client.Object
	var spec *capsulev1beta2.TenantResourceCommonSpec
	var status *capsulev1beta2.TenantResourceCommonStatus
	if global {
		obj := &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"energy": "solar"}}, TenantResourceCommonSpec: common}}
		parent, spec, status = obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
	} else {
		obj := &capsulev1beta2.TenantResource{Name: name, Namespace: baseNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
		parent, spec, status = obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
	}
	Expect(k8sClient.Create(ctx, parent)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, parent)) })
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(status.ProcessedItems).To(HaveLen(1))
		g.Expect(status.ProcessedItems[0].Status).To(Equal(metav1.ConditionTrue))
		g.Expect(status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
		g.Expect(status.ProcessedItems[0].Tenant).To(Equal(tenantName))
		g.Expect(status.ProcessedItems[0].Created).To(BeFalse())
		g.Expect(status.ServiceAccount).NotTo(BeNil())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	By("assigning protection to the lifecycle manager through a skipped apply")
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
			return err
		}
		spec.Resources[0].Policy.Condition = "false"
		spec.Resources[0].Policy.Protect = new(true)
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		g.Expect(status.ProcessedItems).To(HaveLen(1))
		g.Expect(status.ProcessedItems[0].Message).To(Equal(ssa.ConditionNotMet))
		g.Expect(status.ProcessedItems[0].Policy.IsProtected()).To(BeTrue())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	previousPolicy := status.ProcessedItems[0].Policy.DeepCopy()
	sa := status.ServiceAccount
	runner := impersonationClient(serviceAccountUsername(sa.Namespace.String(), sa.Name.String()), serviceAccountGroups(sa.Namespace.String()))
	actor := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
	cm := &corev1.ConfigMap{}
	Expect(k8sClient.Get(ctx, key, cm)).To(Succeed())
	Expect(cm.Labels).To(HaveKeyWithValue(meta.ReplicationProtectionLabel, meta.ValueTrue))

	By("rejecting only the follow-up protection removal while allowing content apply")
	const denial = "e2e injected protection metadata failure"
	binding := blockReplicationMetadataUpdates(name, targetNamespace, []string{name}, fmt.Sprintf("has(object.metadata.labels) && '%s' in object.metadata.labels && object.metadata.labels['%s'] == 'true'", meta.ReplicationProtectionLabel, meta.ReplicationProtectionLabel), denial)
	removeProtection := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":null}}}`, meta.ReplicationProtectionLabel)))
	Eventually(func() error { return runner.Patch(ctx, cm, removeProtection, client.DryRunAll) }, defaultTimeoutInterval, defaultPollInterval).Should(MatchError(ContainSubstring(denial)))
	Eventually(func() error {
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
			return err
		}
		spec.Resources[0].Policy.Condition = ""
		if global {
			spec.Resources[0].Policy.Condition = "true"
		}
		spec.Resources[0].Policy.Protect = new(false)
		spec.Resources[0].Policy.Deletion = apiruntime.ResourceDeletionPolicyRemove
		updated := target.DeepCopy()
		updated.Data["managed"] = "updated"
		spec.Resources[0].RawItems = []capsulev1beta2.RawExtension{{Object: updated}}
		return k8sClient.Update(ctx, parent)
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		g.Expect(status.ProcessedItems).To(HaveLen(1))
		g.Expect(status.ProcessedItems[0].Status).To(Equal(metav1.ConditionFalse))
		g.Expect(status.ProcessedItems[0].Message).To(ContainSubstring(denial))
		g.Expect(status.ProcessedItems[0].Policy).To(Equal(previousPolicy))
		g.Expect(status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
		g.Expect(k8sClient.Get(ctx, key, cm)).To(Succeed())
		g.Expect(cm.Data).To(Equal(map[string]string{"external": "retained", "managed": "updated"}))
		g.Expect(cm.Labels).To(HaveKeyWithValue(meta.ReplicationProtectionLabel, meta.ValueTrue))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

	By("retaining admission protection after the failed policy update")
	Expect(actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"policy-failure":"denied"}}}`)))).To(MatchError(ContainSubstring("is managed by a")))
	Expect(actor.Delete(ctx, cm, client.DryRunAll)).To(MatchError(ContainSubstring("is managed by a")))
	Expect(k8sClient.Get(ctx, key, cm)).To(Succeed())
	Expect(cm.Annotations).NotTo(HaveKey("policy-failure"))
	expectConfigMapData(excludedNamespace, name, map[string]string{"external": "excluded"})

	By("committing the new policy only after a successful retry")
	Expect(k8sClient.Delete(ctx, binding)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(status.ProcessedItems).To(HaveLen(1))
		g.Expect(status.ProcessedItems[0].Status).To(Equal(metav1.ConditionTrue))
		expectedPolicy := spec.Resources[0].Policy.DeepCopy()
		expectedPolicy.Condition = ""
		g.Expect(status.ProcessedItems[0].Policy).To(Equal(expectedPolicy))
		g.Expect(status.ProcessedItems[0].Message).To(BeEmpty())
		g.Expect(k8sClient.Get(ctx, key, cm)).To(Succeed())
		g.Expect(cm.Labels).NotTo(HaveKey(meta.ReplicationProtectionLabel))
		g.Expect(cm.Labels).NotTo(HaveKey(meta.ProtectedByCapsuleLabel))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Eventually(func() error {
		return actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"policy-failure":"allowed"}}}`)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(actor.Delete(ctx, cm, client.DryRunAll)).To(Succeed())
	Expect(k8sClient.Get(ctx, key, cm)).To(Succeed())
	Expect(cm.Annotations).To(HaveKeyWithValue("policy-failure", "allowed"))
	Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	expectConfigMapData(targetNamespace, name, map[string]string{"external": "retained"})
	expectConfigMapData(excludedNamespace, name, map[string]string{"external": "excluded"})
}

// Scope the injected admission failure to this test's target objects only.
func blockReplicationMetadataUpdates(name, namespace string, names []string, expression, message string) *admissionregistrationv1.ValidatingAdmissionPolicyBinding {
	ctx := context.Background()
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{Name: name, Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
		FailurePolicy: new(admissionregistrationv1.Fail),
		MatchConstraints: &admissionregistrationv1.MatchResources{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": namespace}},
			ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{{ResourceNames: names, RuleWithOperations: admissionregistrationv1.RuleWithOperations{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
				Rule:       admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}},
			}}},
		},
		Validations: []admissionregistrationv1.Validation{{
			Expression: expression,
			Message:    message,
		}},
	}}
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{Name: name, Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: name, ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny}}}
	Expect(k8sClient.Create(ctx, policy)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, policy)) })
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, binding)) })
	return binding
}

func exerciseInitialReplicationPolicyFailure(global bool, tenantName, baseNamespace, targetNamespace, excludedNamespace string, owner rbac.UserSpec) {
	ctx := context.Background()
	name := fmt.Sprintf("initial-policy-failure-%t", global)
	created, adopted, removed := name+"-created", name+"-adopted", name+"-removed"
	seed := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: adopted, Namespace: targetNamespace, Data: map[string]string{"external": "retained"}}
	Expect(k8sClient.Patch(ctx, seed, client.Apply, client.FieldOwner("e2e-external"))).To(Succeed())
	Expect(k8sClient.Create(ctx, &corev1.ConfigMap{Name: created, Namespace: excludedNamespace, Data: map[string]string{"external": "excluded"}})).To(Succeed())
	const denial = "e2e injected first-apply metadata failure"
	blockReplicationMetadataUpdates(name, targetNamespace, []string{created, adopted, removed}, fmt.Sprintf("!has(object.metadata.labels) || !('%s' in object.metadata.labels)", meta.NewManagedByCapsuleLabel), denial)
	probe := client.RawPatch(types.MergePatchType, []byte(fmt.Sprintf(`{"metadata":{"labels":{"%s":"replications"}}}`, meta.NewManagedByCapsuleLabel)))
	Eventually(func() error { return k8sClient.Patch(ctx, seed, probe, client.DryRunAll) }, defaultTimeoutInterval, defaultPollInterval).Should(MatchError(ContainSubstring(denial)))
	// Exercise a maximum-length expression once in spec, without copying it into
	// the status of each rendered target.
	condition := "true //" + strings.Repeat("x", 4096-len("true //"))
	block := func(deletion apiruntime.ResourceDeletionPolicy, names ...string) capsulev1beta2.ResourceSpec {
		resource := capsulev1beta2.ResourceSpec{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNamespace}},
			Policy:            &apiruntime.ResourceTemplatePolicy{Condition: condition, Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(true), Deletion: deletion},
		}
		for _, target := range names {
			resource.RawItems = append(resource.RawItems, capsulev1beta2.RawExtension{Object: &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: target, Data: map[string]string{"managed": "retained"}}})
		}
		return resource
	}
	common := capsulev1beta2.TenantResourceCommonSpec{ResyncPeriod: resyncPeriod, Resources: []capsulev1beta2.ResourceSpec{block(apiruntime.ResourceDeletionPolicyOrphan, created, adopted), block(apiruntime.ResourceDeletionPolicyRemove, removed)}}
	var parent client.Object
	var status *capsulev1beta2.TenantResourceCommonStatus
	var spec *capsulev1beta2.TenantResourceCommonSpec
	if global {
		obj := &capsulev1beta2.GlobalTenantResource{Name: name, Spec: capsulev1beta2.GlobalTenantResourceSpec{Scope: api.ResourceScopeNamespace, TenantSelector: metav1.LabelSelector{MatchLabels: map[string]string{"energy": "solar"}}, TenantResourceCommonSpec: common}}
		parent, spec, status = obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
	} else {
		obj := &capsulev1beta2.TenantResource{Name: name, Namespace: baseNamespace, Spec: capsulev1beta2.TenantResourceSpec{TenantResourceCommonSpec: common}}
		parent, spec, status = obj, &obj.Spec.TenantResourceCommonSpec, &obj.Status.TenantResourceCommonStatus
	}
	By("tracking initial content writes with explicit lifecycle policies despite metadata failure")
	Expect(k8sClient.Create(ctx, parent)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, parent)) })
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
		g.Expect(status.ProcessedItems).To(HaveLen(3))
		for _, item := range status.ProcessedItems {
			g.Expect(item.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(item.Message).To(ContainSubstring(denial))
			g.Expect(item.LastApply.IsZero()).To(BeFalse())
			g.Expect(item.Tenant).To(Equal(tenantName))
			g.Expect(item.Policy).NotTo(BeNil())
			g.Expect(item.Policy.Condition).To(BeEmpty())
			g.Expect(item.Policy.IsProtected()).To(BeTrue())
			g.Expect(item.Policy.ShouldOrphan()).To(Equal(item.Name != removed))
			g.Expect(item.Created).To(Equal(item.Name != adopted))
		}
		g.Expect(spec.Resources[0].Policy.Condition).To(Equal(condition))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	actor := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
	By("denying owner changes to both newly created and adopted partial targets")
	for _, target := range []string{created, adopted, removed} {
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: target, Namespace: targetNamespace}, cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("managed", "retained"))
		Expect(actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"initial-policy-failure":"denied"}}}`)))).To(MatchError(ContainSubstring("is managed by a")))
		Expect(actor.Delete(ctx, cm, client.DryRunAll)).To(MatchError(ContainSubstring("is managed by a")))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
		Expect(cm.Annotations).NotTo(HaveKey("initial-policy-failure"))
	}
	By("removing the parent before metadata ever succeeds and respecting Orphan and Remove")
	Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	for _, target := range []string{created, adopted} {
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: target, Namespace: targetNamespace}, cm)).To(Succeed())
		Expect(cm.Data).To(HaveKeyWithValue("managed", "retained"))
		Expect(cm.Labels).NotTo(HaveKey(meta.ReplicationProtectionLabel))
		if target == adopted {
			Expect(cm.Data).To(HaveKeyWithValue("external", "retained"))
		}
		Eventually(func() error {
			return actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"initial-policy-failure":"allowed"}}}`)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(actor.Delete(ctx, cm)).To(Succeed())
	}
	Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKey{Name: removed, Namespace: targetNamespace}, &corev1.ConfigMap{}))).To(BeTrue())
	expectConfigMapData(excludedNamespace, created, map[string]string{"external": "excluded"})
}
