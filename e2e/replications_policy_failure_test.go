// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

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
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{Name: name, Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
		FailurePolicy: new(admissionregistrationv1.Fail),
		MatchConstraints: &admissionregistrationv1.MatchResources{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNamespace}},
			ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{{ResourceNames: []string{name}, RuleWithOperations: admissionregistrationv1.RuleWithOperations{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Update},
				Rule:       admissionregistrationv1.Rule{APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"configmaps"}},
			}}},
		},
		Validations: []admissionregistrationv1.Validation{{
			Expression: fmt.Sprintf("has(object.metadata.labels) && '%s' in object.metadata.labels && object.metadata.labels['%s'] == 'true'", meta.ReplicationProtectionLabel, meta.ReplicationProtectionLabel),
			Message:    denial,
		}},
	}}
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{Name: name, Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{PolicyName: name, ValidationActions: []admissionregistrationv1.ValidationAction{admissionregistrationv1.Deny}}}
	Expect(k8sClient.Create(ctx, policy)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, policy)) })
	Expect(k8sClient.Create(ctx, binding)).To(Succeed())
	DeferCleanup(func() { ignoreNotFound(k8sClient.Delete(ctx, binding)) })
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
		g.Expect(status.ProcessedItems[0].Policy).To(Equal(spec.Resources[0].Policy))
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
