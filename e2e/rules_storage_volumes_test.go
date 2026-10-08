// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("additional PersistentVolume access", Label("tenant", "rules", "storage", "persistentvolumeclaim", "volume-access"), func() {
	It("authorizes restore handoffs while preserving ownership labels and namespace profiles", func() {
		ctx := context.Background()
		staging := NewNamespace("")
		Expect(k8sClient.Create(ctx, staging)).To(Succeed())
		DeferCleanup(EventuallyDeletion, staging)
		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "restorer", Namespace: staging.Name}}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())
		restoreClient := impersonationClient(serviceAccountUsername(sa.Namespace, sa.Name), serviceAccountGroups(sa.Namespace))
		expression := fmt.Sprintf(`request.operation == 'UPDATE' && volume != null && has(volume.spec.claimRef) && volume.spec.claimRef.namespace == '%s' && volume.spec.claimRef.name.startsWith(object.metadata.namespace + '-')`, staging.Name)
		grant := &rules.NamespaceRuleBodyTenant{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"volume-profile": "restore"}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Audience: []rules.Audience{{Kind: rules.AudienceKindGroup, Name: "system:serviceaccounts:" + staging.Name}},
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action:     rules.ActionTypeAllow,
					Conditions: []rules.AdmissionCondition{{Name: "restore-handoff", Expression: expression}},
					Storage:    rules.NamespaceRuleEnforceStorageBody{Volumes: []rules.PersistentVolumeMatch{{Name: "restore-pool", Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"restore-pool": "approved"}}}}},
				},
			},
		}
		tenants := make([]*capsule.Tenant, 0, 2)
		for _, suffix := range []string{"a", "b"} {
			name := staging.Name + "-" + suffix
			tnt := &capsule.Tenant{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"env": "e2e"}}, Spec: capsule.TenantSpec{Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{UserSpec: rbac.UserSpec{Kind: rbac.UserOwner, Name: name}}}}}}
			if suffix == "a" {
				tnt.Spec.Rules = []*rules.NamespaceRuleBodyTenant{grant.DeepCopy()}
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			tenants = append(tenants, tnt)
		}
		a, b := tenants[0], tenants[1]
		ownerA := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		newNS := func(tnt *capsule.Tenant, profile string) *corev1.Namespace {
			ns := NewNamespace("", map[string]string{meta.TenantLabel: tnt.Name, "volume-profile": profile})
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			role := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: ns.Name}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{""}, Resources: []string{"persistentvolumeclaims"}, Verbs: []string{"get", "patch"}}}}
			binding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: role.Name, Namespace: ns.Name}, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: sa.Name, Namespace: sa.Namespace}}}
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())
			return ns
		}
		selected, ordinary, otherTenant := newNS(a, "restore"), newNS(a, "ordinary"), newNS(b, "restore")
		profileReady := func(ns *corev1.Namespace, count int, pool, condition string) {
			Eventually(func(g Gomega) {
				status := &capsule.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(status.Status.Rules).To(HaveLen(count))
				if count > 0 {
					g.Expect(status.Status.Rules[0].Enforce.Storage.Volumes[0].Selector.MatchLabels).To(HaveKeyWithValue("restore-pool", pool))
					g.Expect(status.Status.Rules[0].Enforce.Conditions[0].Expression).To(Equal(condition))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		profileReady(selected, 1, "approved", expression)
		profileReady(ordinary, 0, "", "")
		profileReady(otherTenant, 0, "", "")
		newPV := func(ns *corev1.Namespace, suffix, owner, pool string) *corev1.PersistentVolume {
			pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: ns.Name + "-" + suffix, Labels: map[string]string{"env": "e2e", "restore-pool": pool, "preserve": "original"}}, Spec: corev1.PersistentVolumeSpec{
				Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: "manual", PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				ClaimRef:               &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: staging.Name, Name: ns.Name + "-temporary"},
				PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/capsule-volume-access"}},
			}}
			if owner != "" {
				pv.Labels[meta.TenantLabel] = owner
			}
			Expect(k8sClient.Create(ctx, pv)).To(Succeed())
			DeferCleanup(EventuallyDeletion, pv)
			return pv
		}
		restored := newPV(selected, "restore", "", "approved")
		wrongPool := newPV(selected, "wrong-pool", "", "unapproved")
		own := newPV(selected, "owned", a.Name, "unapproved")
		cross := newPV(selected, "other-tenant", b.Name, "approved")
		ordinaryPV := newPV(ordinary, "restore", "", "approved")
		newPVC := func(ns *corev1.Namespace, name, volume string, owner client.Client) *corev1.PersistentVolumeClaim {
			claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e", "restore-pool": "approved"}}, Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: ptr.To("manual"), Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"velero.io/dynamic-pv-restore": ns.Name}}, VolumeName: volume,
			}}
			Expect(owner.Create(ctx, claim)).To(Succeed())
			DeferCleanup(EventuallyDeletion, claim)
			return claim
		}
		By("retaining normal tenant access without matching the additional selector or audience")
		ownedPVC := newPVC(selected, "owned", own.Name, ownerA)
		currentOwned := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ownedPVC), currentOwned)).To(Succeed())
		Expect(currentOwned.Spec.VolumeName).To(Equal(own.Name))
		claim := newPVC(selected, "destination", "", ownerA)
		ordinaryClaim := newPVC(ordinary, "destination", "", ownerA)
		otherClaim := newPVC(otherTenant, "destination", "", impersonationClient(b.Spec.Owners[0].Name, withDefaultGroups(nil)))
		for _, target := range []*corev1.PersistentVolumeClaim{claim, ordinaryClaim, otherClaim} {
			Eventually(func() error {
				return restoreClient.Get(ctx, client.ObjectKeyFromObject(target), &corev1.PersistentVolumeClaim{})
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		patchClaim := func(actor client.Client, target *corev1.PersistentVolumeClaim, pv *corev1.PersistentVolume, dryRun bool) error {
			current := &corev1.PersistentVolumeClaim{}
			if err := actor.Get(ctx, client.ObjectKeyFromObject(target), current); err != nil {
				return err
			}
			base := current.DeepCopy()
			current.Spec.VolumeName = pv.Name
			options := []client.PatchOption{}
			if dryRun {
				options = append(options, client.DryRunAll)
			}
			return actor.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}), options...)
		}
		reject := func(actor client.Client, target *corev1.PersistentVolumeClaim, pv *corev1.PersistentVolume, reason string) {
			Eventually(func(g Gomega) {
				err := patchClaim(actor, target, pv, false)
				if apierrors.IsConflict(err) {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				Expect(err).To(HaveOccurred(), "unauthorized binding must not persist")
				g.Expect(err).To(MatchError(ContainSubstring(reason)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			current := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(target), current)).To(Succeed())
			Expect(current.Spec.VolumeName).To(BeEmpty())
		}
		By("checking the PV labels, namespace profile and authenticated caller")
		reject(ownerA, claim, restored, "missing the Tenant label")
		reject(restoreClient, claim, wrongPool, "missing the Tenant label")
		reject(restoreClient, ordinaryClaim, ordinaryPV, "missing the Tenant label")
		reject(restoreClient, otherClaim, restored, "missing the Tenant label")
		reject(restoreClient, claim, cross, "cross-tenant mount")
		By("preventing tenant actors from editing the PV labels used for authorization")
		Eventually(func(g Gomega) {
			current := &corev1.PersistentVolume{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), current)).To(Succeed())
			base := current.DeepCopy()
			current.Labels[meta.TenantLabel] = a.Name
			err := ownerA.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
			Expect(err).To(HaveOccurred(), "unauthorized PV label change must not persist")
			g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected PV RBAC rejection: %v", err)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		currentCross := &corev1.PersistentVolume{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(cross), currentCross)).To(Succeed())
		Expect(currentCross.Labels).To(Equal(cross.Labels))
		By("re-evaluating namespace selection and rule changes")
		setProfile := func(value string) {
			Eventually(func() error {
				current := &corev1.Namespace{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ordinary), current); err != nil {
					return err
				}
				current.Labels["volume-profile"] = value
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		setProfile("restore")
		profileReady(ordinary, 1, "approved", expression)
		Eventually(func() error { return patchClaim(restoreClient, ordinaryClaim, ordinaryPV, true) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		setProfile("ordinary")
		profileReady(ordinary, 0, "", "")
		reject(restoreClient, ordinaryClaim, ordinaryPV, "missing the Tenant label")
		setPolicy := func(pool, condition string) {
			Eventually(func() error {
				current := &capsule.Tenant{}
				if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
					return err
				}
				current.Spec.Rules[0].Enforce.Storage.Volumes[0].Selector.MatchLabels["restore-pool"] = pool
				current.Spec.Rules[0].Enforce.Conditions[0].Expression = condition
				return k8sClient.Update(ctx, current)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
			profileReady(selected, 1, pool, condition)
		}
		setPolicy("unapproved", expression)
		reject(restoreClient, claim, restored, "missing the Tenant label")
		setPolicy("approved", "false")
		reject(restoreClient, claim, restored, "missing the Tenant label")
		setPolicy("approved", expression)
		By("admitting the destination PVC patch without modifying either selector or PV labels")
		before := claim.DeepCopy()
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(claim), before)).To(Succeed())
		Eventually(func() error { return patchClaim(restoreClient, claim, restored, false) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &corev1.PersistentVolume{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(restored), current)).To(Succeed())
			g.Expect(current.Labels).To(Equal(restored.Labels))
			g.Expect(current.Spec.ClaimRef.Namespace).To(Equal(staging.Name))
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(claim), claim)).To(Succeed())
			g.Expect(claim.Spec.Selector).To(Equal(before.Spec.Selector))
			g.Expect(claim.Spec.VolumeName).To(Equal(restored.Name))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		By("finishing the trusted PV handoff and preserving unrelated labels")
		Eventually(func() error {
			current := &corev1.PersistentVolume{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(restored), current); err != nil {
				return err
			}
			base := current.DeepCopy()
			current.Spec.ClaimRef = &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: claim.Namespace, Name: claim.Name, UID: claim.UID}
			for key, value := range claim.Spec.Selector.MatchLabels {
				current.Labels[key] = value
			}
			return k8sClient.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &corev1.PersistentVolume{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(restored), current)).To(Succeed())
			g.Expect(current.Labels).To(HaveKeyWithValue(meta.TenantLabel, a.Name))
			g.Expect(current.Labels).To(HaveKeyWithValue("preserve", "original"))
			g.Expect(current.Labels).To(HaveKeyWithValue("restore-pool", "approved"))
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(claim), claim)).To(Succeed())
			g.Expect(claim.Status.Phase).To(Equal(corev1.ClaimBound))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})
})
