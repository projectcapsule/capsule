// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/resourcepermit"
	apiruntime "github.com/projectcapsule/capsule/pkg/api/runtime"
	"github.com/projectcapsule/capsule/pkg/runtime/selectors"
)

func exerciseMixedProtection(global bool, tenantName, baseNamespace, targetNamespace, excludedNamespace string, owner rbac.UserSpec) {
	ctx := context.Background()
	const name = "mixed-protection"
	target := &corev1.ConfigMap{APIVersion: "v1", Kind: "ConfigMap", Name: name, Data: map[string]string{"shared": "managed"}}
	seed := target.DeepCopy()
	seed.Namespace = targetNamespace
	seed.Data = map[string]string{"external": "retained"}
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
		g.Expect(status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
		g.Expect(status.ProcessedItems[0].Tenant).To(Equal(tenantName))
		g.Expect(status.ServiceAccount).NotTo(BeNil())
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	sa := status.ServiceAccount
	bindServiceAccountToClusterResources(sa.Namespace.String(), sa.Name.String(), name, name, []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"namespaces"}, ResourceNames: []string{targetNamespace}, Verbs: []string{"get"}}})
	DeferCleanup(func() { EventuallyDeletion(&rbacv1.ClusterRole{Name: name}) })
	DeferCleanup(func() { EventuallyDeletion(&rbacv1.ClusterRoleBinding{Name: name}) })
	template := &capsulev1beta2.GlobalResourcePermitTemplate{Name: name, Spec: capsulev1beta2.GlobalResourcePermitTemplateSpec{
		Impersonation: resourcePermitServiceAccountReference(sa.Namespace.String(), sa.Name.String()),
		Approvals:     resourcepermit.ApprovalSpec{Auto: true}, DefaultDuration: &metav1.Duration{Duration: 10 * time.Minute},
		NamespaceSelectors: []selectors.NamespaceSelector{{LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": targetNamespace}}}},
		Resources:          []apiruntime.ResourceTemplate{{Policy: apiruntime.ResourceTemplatePolicy{Creation: apiruntime.ResourceCreationPolicyMerge, Protect: new(true), Deletion: apiruntime.ResourceDeletionPolicyRemove}, Targets: []runtime.RawExtension{{Object: target}}}},
	}}
	Expect(k8sClient.Create(ctx, template)).To(Succeed())
	DeferCleanup(func() { EventuallyDeletion(template) })
	expectGlobalResourcePermitTemplateNamespaces(ctx, name, targetNamespace)
	actor := impersonationClient(owner.Name, withDefaultGroups([]string{owner.Name}))
	permit := &capsulev1beta2.ResourcePermit{Name: name, Namespace: targetNamespace, Spec: capsulev1beta2.ResourcePermitSpec{Template: capsulev1beta2.ResourcePermitTemplateReference{Kind: capsulev1beta2.GlobalResourcePermitTemplateKind, Name: name}}}
	Expect(actor.Create(ctx, permit)).To(Succeed())
	DeferCleanup(func() { cleanupLifecycleResourcePermit(ctx, permit) })
	permit = waitForResourcePermitPhase(ctx, permit, capsulev1beta2.ResourcePermitPhaseActive)
	// The controller identity must observe its completed SSA write, just as an
	// impersonated execution client does, before publishing lifecycle status.
	Expect(permit.Status.ProcessedItems).To(HaveLen(1))
	Expect(permit.Status.ProcessedItems[0].LastApply.IsZero()).To(BeFalse())
	Expect(permit.Status.ProcessedItems[0].Policy.IsProtected()).To(BeTrue())
	By("enabling replication protection on a target already protected by a permit")
	setProtection := func(protect, cordoned bool) {
		Eventually(func() error {
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent); err != nil {
				return err
			}
			spec.Resources[0].Policy.Condition = "false"
			spec.Resources[0].Policy.Protect = new(protect)
			spec.Cordoned = new(cordoned)
			return k8sClient.Update(ctx, parent)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent)).To(Succeed())
			g.Expect(status.ObservedGeneration).To(Equal(parent.GetGeneration()))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	}
	setProtection(true, false)
	cm := &corev1.ConfigMap{}
	assertPermitProtected := func() {
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(seed), cm)).To(Succeed())
		Expect(cm.Labels).To(HaveKeyWithValue(meta.ResourcePermitProtectionLabel, meta.ValueTrue))
		Expect(cm.Annotations).To(HaveKeyWithValue(meta.ResourcePermitServiceAccountAnnotation, serviceAccountUsername(sa.Namespace.String(), sa.Name.String())))
		Expect(actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":null,"annotations":null}}`)))).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
		Expect(actor.Delete(ctx, cm, client.DryRunAll)).To(MatchError(ContainSubstring("protected by a ResourcePermit")))
	}
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(seed), cm)).To(Succeed())
	Expect(cm.Labels).To(HaveKeyWithValue(meta.ProtectedByCapsuleLabel, meta.ValueControllerResourcePermit))
	Expect(cm.Labels).To(HaveKeyWithValue(meta.ReplicationProtectionLabel, meta.ValueTrue))
	setProtection(false, false)
	assertPermitProtected()
	Expect(cm.Labels).NotTo(HaveKey(meta.ReplicationProtectionLabel))
	setProtection(true, false)
	if global {
		By("retaining replication protection after the permit expires while replication is cordoned")
		setProtection(true, true)
		expireActiveResourcePermit(ctx, permit)
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(permit), permit))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(seed), cm)).To(Succeed())
		Expect(cm.Labels).To(HaveKeyWithValue(meta.ReplicationProtectionLabel, meta.ValueTrue))
		Expect(cm.Labels).NotTo(HaveKey(meta.ResourcePermitProtectionLabel))
		Expect(cm.Annotations).NotTo(HaveKey(meta.ResourcePermitServiceAccountAnnotation))
		Expect(actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"labels":null}}`)))).To(MatchError(ContainSubstring("is managed by a")))
		Expect(actor.Delete(ctx, cm, client.DryRunAll)).To(MatchError(ContainSubstring("is managed by a")))
	}
	By("orphaning the replication without clearing any surviving permit protection")
	Expect(k8sClient.Delete(ctx, parent)).To(Succeed())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(parent), parent))
	}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	if !global {
		assertPermitProtected()
		expireActiveResourcePermit(ctx, permit)
		Eventually(func() bool {
			return apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(permit), permit))
		}, defaultTimeoutInterval, defaultPollInterval).Should(BeTrue())
	}
	Eventually(func() error {
		return actor.Patch(ctx, cm, client.RawPatch(types.MergePatchType, []byte(`{"metadata":{"annotations":{"allowed":"true"}}}`)))
	}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	Expect(actor.Delete(ctx, cm)).To(Succeed())
	expectConfigMapData(excludedNamespace, name, map[string]string{"external": "excluded"})
}
