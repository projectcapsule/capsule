// Copyright 2020-2026 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	storagev1beta1 "k8s.io/api/storage/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsule "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
	"github.com/projectcapsule/capsule/pkg/utils"
)

var _ = Describe("VolumeAttributesClass defaults", Label("tenant", "rules", "mutation", "storage", "persistentvolumeclaim", "volumeattributesclass"), func() {
	It("defaults attribute classes on claim creation per namespace profile and tenant", func() {
		ctx := context.Background()

		// Kubernetes drops the class of a claim when it serves no VolumeAttributesClass API.
		if err := k8sClient.List(ctx, &storagev1.VolumeAttributesClassList{}); err != nil {
			Expect(utils.IsUnsupportedAPI(err)).To(BeTrue(), "listing VolumeAttributesClasses: %v", err)
			if err := k8sClient.List(ctx, &storagev1beta1.VolumeAttributesClassList{}); err != nil {
				Expect(utils.IsUnsupportedAPI(err)).To(BeTrue(), "listing VolumeAttributesClasses: %v", err)
				Skip(fmt.Sprintf("VolumeAttributesClass API is not served: %s", err))
			}
		}

		prefix := "e2e-" + rand.String(6)
		className := func(name string) string { return prefix + "-" + name }
		storageClass := className("manual")

		merge := rules.NamespaceRuleBodyTenant{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"attribute-default": prefix}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Mutate: []rules.NamespaceRuleMutation{{Storage: rules.StorageMutation{AttributeClass: "{{ .tenant.metadata.name }}-gold"}}},
				Enforce: &rules.NamespaceRuleEnforceBody{
					Action:  rules.ActionTypeAllow,
					Storage: rules.NamespaceRuleEnforceStorageBody{AttributeClasses: []rules.StorageClassMatch{{Exact: []string{"{{ .tenant.metadata.name }}-gold", className("silver")}}}},
				},
			},
		}
		replace := rules.NamespaceRuleBodyTenant{
			NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"attribute-forced": prefix}},
			NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
				Mutate: []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Storage: rules.StorageMutation{AttributeClass: className("silver")}}},
			},
		}
		owners := rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
			Audience: []rules.Audience{{Kind: rules.AudienceKindCustom, Name: string(rules.CustomAudienceTenantOwner)}},
			Mutate:   []rules.NamespaceRuleMutation{{Action: rules.MutationActionReplace, Storage: rules.StorageMutation{AttributeClass: className("bronze")}}},
		}}

		newTenant := func(suffix string, profiles ...rules.NamespaceRuleBodyTenant) *capsule.Tenant {
			name := prefix + "-" + suffix
			tnt := &capsule.Tenant{
				Name: name, Labels: map[string]string{"env": "e2e"},
				Spec: capsule.TenantSpec{Owners: rbac.OwnerListSpec{{Kind: rbac.UserOwner, Name: name}}},
			}
			for _, profile := range profiles {
				tnt.Spec.Rules = append(tnt.Spec.Rules, profile.DeepCopy())
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(DeleteInBackground, tnt)
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			return tnt
		}
		a, b := newTenant("a", merge, replace), newTenant("b", owners)
		ownerA := impersonationClient(a.Spec.Owners[0].Name, withDefaultGroups(nil))
		ownerB := impersonationClient(b.Spec.Owners[0].Name, withDefaultGroups(nil))
		tenantGold := a.Name + "-gold"

		newNS := func(tnt *capsule.Tenant, labels map[string]string) *corev1.Namespace {
			all := map[string]string{meta.TenantLabel: tnt.Name}
			maps.Copy(all, labels)
			ns := NewNamespace("", all)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			NamespaceIsPartOfTenant(tnt, ns).Should(Succeed())
			return ns
		}
		defaulted := newNS(a, map[string]string{"attribute-default": prefix})
		forced := newNS(a, map[string]string{"attribute-forced": prefix})
		open := newNS(a, nil)
		other := newNS(b, nil)

		// profileReady waits until a namespace's rules set the class, or none.
		profileReady := func(ns *corev1.Namespace, class string) {
			Eventually(func(g Gomega) {
				status := &capsule.RuleStatus{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
				g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
				ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
				g.Expect(ready).NotTo(BeNil())
				g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				classes := []string{}
				for _, rule := range status.Status.Rules {
					for _, mutation := range rule.Mutate {
						if mutation.Storage.AttributeClass != "" {
							classes = append(classes, mutation.Storage.AttributeClass)
						}
					}
				}
				if class == "" {
					g.Expect(classes).To(BeEmpty())
				} else {
					g.Expect(classes).To(Equal([]string{class}))
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
		profileReady(defaulted, tenantGold)
		profileReady(forced, className("silver"))
		profileReady(open, "")
		profileReady(other, className("bronze"))

		claimSpec := func(class *string) corev1.PersistentVolumeClaimSpec {
			return corev1.PersistentVolumeClaimSpec{
				AccessModes:               []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName:          new(storageClass),
				Resources:                 corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
				VolumeAttributesClassName: class,
			}
		}
		claim := func(ns *corev1.Namespace, name string, class *string) *corev1.PersistentVolumeClaim {
			return &corev1.PersistentVolumeClaim{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}, Spec: claimSpec(class)}
		}
		// created creates a claim and returns the class Kubernetes stored.
		created := func(actor client.Client, obj *corev1.PersistentVolumeClaim) *string {
			EventuallyCreation(func() error { return actor.Create(ctx, obj.DeepCopy()) }).Should(Succeed())
			stored := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), stored)).To(Succeed())
			return stored.Spec.VolumeAttributesClassName
		}
		// controllerClaim returns the class of the claim the StatefulSet
		// controller creates from a template without a class.
		controllerClaim := func(actor client.Client, ns *corev1.Namespace, name string) *string {
			labels := map[string]string{"app": name}
			statefulSet := &appsv1.StatefulSet{
				Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: new(int32(1)),
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							SecurityContext: nobodyPodSecurityContext(),
							Containers:      []corev1.Container{{Name: "pause", Image: "registry.k8s.io/pause:3.10", SecurityContext: restrictedContainerSecurityContext()}},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{Name: "data", Spec: claimSpec(nil)}},
				},
			}
			EventuallyCreation(func() error { return actor.Create(ctx, statefulSet.DeepCopy()) }).Should(Succeed())
			stored := &corev1.PersistentVolumeClaim{}
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: "data-" + name + "-0"}, stored)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			return stored.Spec.VolumeAttributesClassName
		}

		By("merging the tenant's class into a claim without one")
		Expect(created(ownerA, claim(defaulted, "omitted", nil))).To(Equal(new(tenantGold)))

		By("keeping an explicit class, and an empty one that chooses none")
		Expect(created(ownerA, claim(defaulted, "explicit", new(className("silver"))))).To(Equal(new(className("silver"))))
		Expect(created(ownerA, claim(defaulted, "empty", new("")))).To(Equal(new("")))

		By("checking a class the default kept against the namespace's class rules")
		Eventually(func(g Gomega) {
			err := ownerA.Create(ctx, claim(defaulted, "forbidden", new(className("bronze"))))
			Expect(err).To(HaveOccurred(), "a forbidden class must not persist")
			g.Expect(apierrors.IsForbidden(err)).To(BeTrue(), "expected an admission denial: %v", err)
			g.Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("volume attributes class %q at spec.volumeAttributesClassName is not allowed by namespace rule", className("bronze")))))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		By("replacing every class in a namespace that forces one")
		Expect(created(ownerA, claim(forced, "omitted", nil))).To(Equal(new(className("silver"))))
		Expect(created(ownerA, claim(forced, "explicit", new(className("gold"))))).To(Equal(new(className("silver"))))
		Expect(created(ownerA, claim(forced, "empty", new("")))).To(Equal(new(className("silver"))))

		By("leaving claims in a namespace without a default unchanged")
		Expect(created(ownerA, claim(open, "omitted", nil))).To(BeNil())

		By("defaulting the claims the StatefulSet controller creates")
		Expect(controllerClaim(ownerA, defaulted, "database")).To(Equal(new(tenantGold)))

		By("applying a default for tenant owners only to their own requests")
		Expect(created(ownerB, claim(other, "owner", nil))).To(Equal(new(className("bronze"))))
		Expect(created(ownerB, claim(other, "explicit", new(className("gold"))))).To(Equal(new(className("bronze"))))
		Expect(created(k8sClient, claim(other, "administrator", nil))).To(BeNil())
		// The controller creates the claim under its own identity.
		Expect(controllerClaim(ownerB, other, "database")).To(BeNil())

		By("previewing the default with a server-side dry run")
		preview := claim(defaulted, "preview", nil)
		Expect(ownerA.Create(ctx, preview, client.DryRunAll)).To(Succeed())
		Expect(preview.Spec.VolumeAttributesClassName).To(Equal(new(tenantGold)))
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(preview), &corev1.PersistentVolumeClaim{}))).To(BeTrue())

		By("never changing the class of an existing claim")
		update := func(ns *corev1.Namespace, name string) *string {
			Eventually(func() error {
				obj := &corev1.PersistentVolumeClaim{}
				if err := ownerA.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: name}, obj); err != nil {
					return err
				}
				obj.Labels["retention"] = "weekly"
				return ownerA.Update(ctx, obj)
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			stored := &corev1.PersistentVolumeClaim{}
			Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: name}, stored)).To(Succeed())
			Expect(stored.Labels).To(HaveKeyWithValue("retention", "weekly"))
			return stored.Spec.VolumeAttributesClassName
		}
		Expect(update(forced, "omitted")).To(Equal(new(className("silver"))))
		Expect(update(defaulted, "empty")).To(Equal(new("")))

		By("applying a changed default to new claims only")
		Eventually(func() error {
			tnt := &capsule.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(a), tnt); err != nil {
				return err
			}
			tnt.Spec.Rules[0].Mutate[0].Storage.AttributeClass = className("silver")
			return k8sClient.Update(ctx, tnt)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		profileReady(defaulted, className("silver"))
		Expect(created(ownerA, claim(defaulted, "after-change", nil))).To(Equal(new(className("silver"))))
		stored := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: defaulted.Name, Name: "omitted"}, stored)).To(Succeed())
		Expect(stored.Spec.VolumeAttributesClassName).To(Equal(new(tenantGold)))

		By("re-evaluating namespace selection")
		Eventually(func() error {
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(open), ns); err != nil {
				return err
			}
			ns.Labels["attribute-forced"] = prefix
			return k8sClient.Update(ctx, ns)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		profileReady(open, className("silver"))
		Expect(created(ownerA, claim(open, "after-selection", new(className("gold"))))).To(Equal(new(className("silver"))))
	})
})
