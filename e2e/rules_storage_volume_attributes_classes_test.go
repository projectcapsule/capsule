// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("VolumeAttributesClass rules", Label("tenant", "rules", "storage", "persistentvolumeclaim", "volumeattributesclass"), func() {
	It("enforces attribute classes per namespace profile and tenant", func() {
		ctx := context.Background()

		if err := k8sClient.List(ctx, &storagev1.VolumeAttributesClassList{}); err != nil && utils.IsUnsupportedAPI(err) {
			Skip(fmt.Sprintf("VolumeAttributesClass API is not served: %s", err))
		}

		prefix := "e2e-" + rand.String(6)
		className := func(name string) string { return prefix + "-" + name }
		const driver = "e2e.csi.example.com"
		storageClass := className("manual")
		for name, tier := range map[string]string{"gold": "customer", "silver": "", "bronze": "internal"} {
			class := &storagev1.VolumeAttributesClass{
				Name: className(name), Labels: map[string]string{"env": "e2e"},
				DriverName: driver,
				Parameters: map[string]string{"iops": "3000"},
			}
			if tier != "" {
				class.Labels["attribute-tier"] = tier
			}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())
			DeferCleanup(DeleteInBackground, class)
		}

		allow := rules.NamespaceRuleBodyTenant{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"attribute-profile": prefix}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{Enforce: &rules.NamespaceRuleEnforceBody{
				Action: rules.ActionTypeAllow,
				Storage: rules.NamespaceRuleEnforceStorageBody{AttributeClasses: []rules.StorageClassMatch{
					{Exact: []string{className("silver")}},
					{Expression: "^" + prefix + "-csi-"},
					{MatchLabels: map[string]string{"attribute-tier": "customer"}},
				}},
			}},
		}
		deny := rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
			Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}},
			Enforce: &rules.NamespaceRuleEnforceBody{
				Action:  rules.ActionTypeDeny,
				Storage: rules.NamespaceRuleEnforceStorageBody{AttributeClasses: []rules.StorageClassMatch{{Exact: []string{className("gold")}}}},
			},
		}}

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
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			return tnt
		}
		a, b := newTenant("a", allow), newTenant("b", deny)
		ownerA := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Spec.Owners[0].Name, withDefaultGroups(nil))

		newNS := func(tnt *capsule.Tenant, profile string) *corev1.Namespace {
			labels := map[string]string{meta.TenantLabel: tnt.Name}
			if profile != "" {
				labels["attribute-profile"] = profile
			}
			ns := NewNamespace("", labels)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
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
					g.Expect(status.Status.Rules[0].Enforce.Storage.AttributeClasses).To(HaveLen(classes))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		profileReady(restricted, 1, 3)
		profileReady(open, 0, 0)
		profileReady(other, 1, 1)

		claimSpec := func(class *string) corev1.PersistentVolumeClaimSpec {
			return corev1.PersistentVolumeClaimSpec{
				AccessModes:               []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName:          new(storageClass),
				Resources:                 corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
				VolumeAttributesClassName: class,
			}
		}
		claim := func(ns *corev1.Namespace, name string, class *string) *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{
				Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"},
				Spec: claimSpec(class),
			}
		}
		admit := func(actor client.Client, obj client.Object) {
			EventuallyCreation(func() error { return actor.Create(ctx, obj.DeepCopyObject().(client.Object)) }).Should(Succeed())
		}
		reject := func(actor client.Client, obj *corev1.PersistentVolumeClaim, reason string) {
			Eventually(func(g Gomega) {
				err := actor.Create(ctx, obj.DeepCopy())
				Expect(err).To(HaveOccurred(), "a forbidden PersistentVolumeClaim must not persist")
				g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected an admission denial: %v", err)
				g.Expect(err).To(MatchError(ContainSubstring(reason)))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), &corev1.PersistentVolumeClaim{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "rejected PersistentVolumeClaim exists: %v", err)
		}
		notAllowed := func(name string) string {
			return fmt.Sprintf("volume attributes class %q at spec.volumeAttributesClassName is not allowed by namespace rule", className(name))
		}

		By("allowing classes matched by name, expression and class labels")
		admit(ownerA, claim(restricted, "silver", new(className("silver"))))
		admit(ownerA, claim(restricted, "expression", new(className("csi-fast"))))
		admit(ownerA, claim(restricted, "labels", new(className("gold"))))

		By("rejecting other classes, including one whose labels can't be read because it doesn't exist")
		reject(ownerA, claim(restricted, "bronze", new(className("bronze"))), notAllowed("bronze"))
		reject(ownerA, claim(restricted, "ghost", new(className("ghost"))), notAllowed("ghost"))

		By("admitting a claim without a class, to which Kubernetes applies none")
		admit(ownerA, claim(restricted, "none", nil))

		By("leaving other namespace profiles and tenants to their own rules")
		admit(ownerA, claim(open, "bronze", new(className("bronze"))))
		admit(ownerB, claim(other, "bronze", new(className("bronze"))))
		reject(ownerB, claim(other, "gold", new(className("gold"))), fmt.Sprintf("volume attributes class %q at spec.volumeAttributesClassName is denied by namespace rule", className("gold")))

		By("checking the claims the StatefulSet controller creates from a template")
		statefulSet := func(name, class string) *appsv1.StatefulSet {
			labels := map[string]string{"app": name}
			return &appsv1.StatefulSet{
				Name: name, Namespace: restricted.Name, Labels: map[string]string{"env": "e2e"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To[int32](1),
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							SecurityContext: nobodyPodSecurityContext(),
							Containers:      []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{Name: "data", Spec: claimSpec(new(className(class)))}},
				},
			}
		}
		admit(ownerA, statefulSet("allowed", "silver"))
		Eventually(func(g Gomega) {
			created := &corev1.PersistentVolumeClaim{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: restricted.Name, Name: "data-allowed-0"}, created)).To(Succeed())
			g.Expect(created.Spec.VolumeAttributesClassName).To(Equal(new(className("silver"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		admit(ownerA, statefulSet("forbidden", "bronze"))
		Eventually(func(g Gomega) {
			list := &corev1.EventList{}
			g.Expect(k8sClient.List(ctx, list, client.InNamespace(restricted.Name))).To(Succeed())
			messages := make([]string, 0, len(list.Items))
			for _, event := range list.Items {
				if event.InvolvedObject.Kind == "StatefulSet" && event.InvolvedObject.Name == "forbidden" && event.Reason == "FailedCreate" {
					messages = append(messages, event.Message)
				}
			}
			g.Expect(messages).To(ContainElement(ContainSubstring(notAllowed("bronze"))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		err := k8sClient.Get(ctx, client.ObjectKey{Namespace: restricted.Name, Name: "data-forbidden-0"}, &corev1.PersistentVolumeClaim{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the StatefulSet controller created a forbidden claim: %v", err)

		By("binding a claim, whose class can then change")
		pv := &corev1.PersistentVolume{
			Name: className("data"), Labels: map[string]string{"env": "e2e", meta.TenantLabel: a.Name},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName:              storageClass,
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				ClaimRef:                      &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: restricted.Name, Name: "data"},
				PersistentVolumeSource:        corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{Driver: driver, VolumeHandle: className("data")}},
			},
		}
		Expect(k8sClient.Create(ctx, pv)).To(Succeed())
		DeferCleanup(DeleteInBackground, pv)
		data := claim(restricted, "data", nil)
		data.Spec.VolumeName = pv.Name
		admit(ownerA, data)
		key := client.ObjectKeyFromObject(data)
		current := func() *corev1.PersistentVolumeClaim {
			obj := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())
			return obj
		}
		Eventually(func(g Gomega) {
			obj := &corev1.PersistentVolumeClaim{}
			g.Expect(k8sClient.Get(ctx, key, obj)).To(Succeed())
			g.Expect(obj.Status.Phase).To(Equal(corev1.ClaimBound))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		update := func(mutate func(*corev1.PersistentVolumeClaim)) error {
			obj := &corev1.PersistentVolumeClaim{}
			if err := ownerA.Get(ctx, key, obj); err != nil {
				return err
			}
			mutate(obj)
			return ownerA.Update(ctx, obj)
		}
		setClass := func(class string) error {
			return update(func(obj *corev1.PersistentVolumeClaim) { obj.Spec.VolumeAttributesClassName = new(className(class)) })
		}
		changeTo := func(class string) {
			Eventually(func() error { return setClass(class) }, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(current().Spec.VolumeAttributesClassName).To(Equal(new(className(class))))
		}
		rejectChange := func(class string, kept *string) {
			Eventually(func(g Gomega) {
				err := setClass(class)
				if apierrors.IsConflict(err) {
					g.Expect(err).NotTo(HaveOccurred())
					return
				}
				Expect(err).To(HaveOccurred(), "a forbidden class change must not persist")
				g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected an admission denial: %v", err)
				g.Expect(err).To(MatchError(ContainSubstring(notAllowed(class))))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Expect(current().Spec.VolumeAttributesClassName).To(Equal(kept))
		}
		// The external-resizer records the class it applied, and why a
		// modification failed. The e2e setup has no CSI driver, so the status
		// is set the same way.
		setStatus := func(applied string, modify *corev1.ModifyVolumeStatus) {
			Eventually(func() error {
				obj := &corev1.PersistentVolumeClaim{}
				if err := k8sClient.Get(ctx, key, obj); err != nil {
					return err
				}
				base := obj.DeepCopy()
				obj.Status.CurrentVolumeAttributesClassName = new(className(applied))
				obj.Status.ModifyVolumeStatus = modify
				return k8sClient.Status().Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}

		By("validating class changes of the bound claim")
		rejectChange("bronze", nil)
		changeTo("gold")
		setStatus("gold", nil)

		By("re-evaluating rule changes without blocking updates that keep the class")
		Eventually(func() error {
			tnt := &capsule.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), tnt); err != nil {
				return err
			}
			tnt.Spec.Rules[0].Enforce.Storage.AttributeClasses = allow.Enforce.Storage.AttributeClasses[:2]
			return k8sClient.Update(ctx, tnt)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		TenantReady(a, metav1.ConditionTrue, defaultTimeoutInterval)
		profileReady(restricted, 1, 2)
		reject(ownerA, claim(restricted, "gold-again", new(className("gold"))), notAllowed("gold"))
		Eventually(func() error {
			return update(func(obj *corev1.PersistentVolumeClaim) { obj.Labels["retention"] = "weekly" })
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(current().Labels).To(HaveKeyWithValue("retention", "weekly"))
		Expect(current().Spec.VolumeAttributesClassName).To(Equal(new(className("gold"))))

		By("cancelling a modification by restoring the applied class the rules no longer allow")
		changeTo("silver")
		setStatus("gold", &corev1.ModifyVolumeStatus{TargetVolumeAttributesClassName: className("silver"), Status: corev1.PersistentVolumeClaimModifyVolumeInfeasible})
		changeTo("gold")
		rejectChange("bronze", new(className("gold")))

		By("re-evaluating namespace selection")
		Eventually(func() error {
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(open), ns); err != nil {
				return err
			}
			ns.Labels["attribute-profile"] = prefix
			return k8sClient.Update(ctx, ns)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		profileReady(open, 1, 2)
		reject(ownerA, claim(open, "bronze-again", new(className("bronze"))), notAllowed("bronze"))
		admit(ownerA, claim(open, "silver", new(className("silver"))))
	})
})
