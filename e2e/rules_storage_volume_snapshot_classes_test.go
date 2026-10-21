// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("VolumeSnapshot class rules", Label("tenant", "rules", "storage", "volumesnapshot"), func() {
	It("enforces snapshot classes per namespace profile and tenant", func() {
		ctx := context.Background()
		utilruntime.Must(snapshotv1.AddToScheme(scheme.Scheme))

		if err := k8sClient.List(ctx, &snapshotv1.VolumeSnapshotClassList{}); err != nil && utils.IsUnsupportedAPI(err) {
			Skip(fmt.Sprintf("VolumeSnapshot API is not installed: %s", err))
		}

		prefix := "e2e-" + rand.String(6)
		className := func(name string) string { return prefix + "-" + name }
		for name, tier := range map[string]string{"gold": "customer", "silver": "", "bronze": "internal"} {
			class := &snapshotv1.VolumeSnapshotClass{
				Name: className(name), Labels: map[string]string{"env": "e2e"},
				Driver:         "hostpath.csi.k8s.io",
				DeletionPolicy: snapshotv1.VolumeSnapshotContentDelete,
			}
			if tier != "" {
				class.Labels["snapshot-tier"] = tier
			}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(EventuallyDeletion, class)
		}

		allow := rules.NamespaceRuleBodyTenant{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"snapshot-profile": prefix}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeAllow,
				Storage: rules.NamespaceRuleEnforceStorageBody{
					Targets: []rules.StorageValidationTarget{rules.StorageTargetVolumeSnapshot},
					Classes: []rules.StorageClassMatch{
						{Exact: []string{className("silver")}},
						{Expression: "^" + prefix + "-csi-"},
						{MatchLabels: map[string]string{"snapshot-tier": "customer"}},
					},
				},
			}},
		}
		deny := rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
			Action:  rules.ActionTypeDeny,
			Storage: rules.NamespaceRuleEnforceStorageBody{Classes: []rules.StorageClassMatch{{Exact: []string{className("gold")}}}},
		}}}

		newTenant := func(suffix string, rule rules.NamespaceRuleBodyTenant) *capsule.Tenant {
			name := prefix + "-" + suffix
			tnt := &capsule.Tenant{
				Name: name, Labels: map[string]string{"env": "e2e"},
				Spec: capsule.TenantSpec{
					Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}},
					Rules:  []*rules.NamespaceRuleBodyTenant{rule.DeepCopy()},
				},
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(EventuallyDeletion, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			return tnt
		}
		a, b := newTenant("a", allow), newTenant("b", deny)
		ownerA := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Spec.Owners[0].Name, withDefaultGroups(nil))

		newNS := func(tnt *capsule.Tenant, profile string) *corev1.Namespace {
			labels := map[string]string{meta.TenantLabel: tnt.Name}
			if profile != "" {
				labels["snapshot-profile"] = profile
			}
			ns := NewNamespace("", labels)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			role := &rbacv1.Role{Name: "snapshots", Namespace: ns.Name, Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{snapshotv1.GroupName}, Resources: []string{"volumesnapshots"}, Verbs: []string{"get", "list", "create", "update", "patch", "delete"},
			}}}
			binding := &rbacv1.RoleBinding{
				Name: role.Name, Namespace: ns.Name,
				RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: role.Name},
				Subjects: []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: rbacv1.UserKind, Name: tnt.Spec.Owners[0].Name}},
			}
			Expect(k8sClient.Create(ctx, role)).To(Succeed())
			Expect(k8sClient.Create(ctx, binding)).To(Succeed())
			return ns
		}
		restricted, open, other := newNS(a, prefix), newNS(a, ""), newNS(b, "")

		profileReady := func(ns *corev1.Namespace, count int, classes int) {
			Eventually(func(g Gomega) {
				status := &capsule.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(status.Status.Rules).To(HaveLen(count))
				if count > 0 {
					g.Expect(status.Status.Rules[0].Enforce.Storage.Classes).To(HaveLen(classes))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		profileReady(restricted, 1, 3)
		profileReady(open, 0, 0)
		profileReady(other, 1, 1)

		snapshot := func(ns *corev1.Namespace, name string, class *string) *snapshotv1.VolumeSnapshot {
			return &snapshotv1.VolumeSnapshot{
				Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"},
				Spec: snapshotv1.VolumeSnapshotSpec{
					Source:                  snapshotv1.VolumeSnapshotSource{PersistentVolumeClaimName: new("data")},
					VolumeSnapshotClassName: class,
				},
			}
		}
		admit := func(actor client.Client, obj *snapshotv1.VolumeSnapshot) {
			EventuallyCreation(func() error { return actor.Create(ctx, obj.DeepCopy()) }).Should(Succeed())
		}
		reject := func(actor client.Client, obj *snapshotv1.VolumeSnapshot, reason string) {
			Eventually(func(g Gomega) {
				err := actor.Create(ctx, obj.DeepCopy())
				Expect(err).To(HaveOccurred(), "a forbidden VolumeSnapshot must not persist")
				g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected an admission denial: %v", err)
				g.Expect(err).To(MatchError(ContainSubstring(reason)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &snapshotv1.VolumeSnapshot{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "rejected VolumeSnapshot exists: %v", err)
		}
		notAllowed := func(name string) string {
			return fmt.Sprintf("volume snapshot class %q at spec.volumeSnapshotClassName is not allowed by namespace rule", className(name))
		}
		const required = "volume snapshot class is required at spec.volumeSnapshotClassName"

		By("allowing classes matched by name, expression and class labels")
		admit(ownerA, snapshot(restricted, "silver", new(className("silver"))))
		admit(ownerA, snapshot(restricted, "expression", new(className("csi-hostpath"))))
		admit(ownerA, snapshot(restricted, "labels", new(className("gold"))))
		kept := snapshot(restricted, "kept", new(className("silver")))
		admit(ownerA, kept)

		By("rejecting other classes and a class left to the cluster default")
		reject(ownerA, snapshot(restricted, "bronze", new(className("bronze"))), notAllowed("bronze"))
		reject(ownerA, snapshot(restricted, "default", nil), required)

		By("admitting a pre-provisioned snapshot, which never receives a default class")
		preProvisioned := snapshot(restricted, "pre-provisioned", nil)
		preProvisioned.Spec.Source = snapshotv1.VolumeSnapshotSource{VolumeSnapshotContentName: new(prefix + "-content")}
		admit(ownerA, preProvisioned)

		By("leaving other namespace profiles and tenants to their own rules")
		admit(ownerA, snapshot(open, "bronze", new(className("bronze"))))
		admit(ownerA, snapshot(open, "default", nil))
		admit(ownerB, snapshot(other, "bronze", new(className("bronze"))))
		reject(ownerB, snapshot(other, "gold", new(className("gold"))), fmt.Sprintf("volume snapshot class %q at spec.volumeSnapshotClassName is denied by namespace rule", className("gold")))
		reject(ownerB, snapshot(other, "default", nil), required)

		By("validating class changes on update")
		changeClass := func(obj *snapshotv1.VolumeSnapshot, class string) error {
			current := &snapshotv1.VolumeSnapshot{}
			if err := ownerA.Get(ctx, client.ObjectKeyFromObject(obj), current); err != nil {
				return err
			}
			current.Spec.VolumeSnapshotClassName = new(class)
			return ownerA.Update(ctx, current)
		}
		changed := snapshot(restricted, "silver", new(className("silver")))
		Eventually(func(g Gomega) {
			err := changeClass(changed, className("bronze"))
			if apierrors.IsConflict(err) {
				g.Expect(err).NotTo(HaveOccurred())
				return
			}
			Expect(err).To(HaveOccurred(), "a forbidden class change must not persist")
			g.Expect(err).To(MatchError(ContainSubstring(notAllowed("bronze"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		current := &snapshotv1.VolumeSnapshot{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(changed), current)).To(Succeed())
		Expect(current.Spec.VolumeSnapshotClassName).To(Equal(new(className("silver"))))
		Eventually(func() error { return changeClass(changed, className("gold")) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("re-evaluating rule changes without blocking updates that keep the class")
		Eventually(func() error {
			current := &capsule.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
				return err
			}
			current.Spec.Rules[0].Enforce.Storage.Classes = current.Spec.Rules[0].Enforce.Storage.Classes[1:]
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
		profileReady(restricted, 1, 2)
		reject(ownerA, snapshot(restricted, "silver-again", new(className("silver"))), notAllowed("silver"))
		Eventually(func() error {
			current := &snapshotv1.VolumeSnapshot{}
			if err := ownerA.Get(ctx, client.ObjectKeyFromObject(kept), current); err != nil {
				return err
			}
			current.Labels["retention"] = "weekly"
			return ownerA.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kept), current)).To(Succeed())
		Expect(current.Labels).To(HaveKeyWithValue("retention", "weekly"))
		Expect(current.Spec.VolumeSnapshotClassName).To(Equal(new(className("silver"))))

		By("re-evaluating namespace selection")
		Eventually(func() error {
			current := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(open), current); err != nil {
				return err
			}
			current.Labels["snapshot-profile"] = prefix
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		profileReady(open, 1, 2)
		reject(ownerA, snapshot(open, "bronze-again", new(className("bronze"))), notAllowed("bronze"))
		admit(ownerA, snapshot(open, "labels", new(className("gold"))))
	})
})
