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
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api/meta"
	"github.com/projectcapsule/capsule/pkg/api/rbac"
	"github.com/projectcapsule/capsule/pkg/api/rules"
)

var _ = Describe("PVC selector immutability during restore", Label("config", "tenant", "storage", "persistentvolumeclaim", "pvc-selector-update"), func() {
	var tenantA, tenantB *capsulev1beta2.Tenant
	var ns *corev1.Namespace
	var owner, restorer client.Client

	BeforeEach(func() {
		ctx := context.Background()
		newTenant := func(suffix string) *capsulev1beta2.Tenant {
			tnt := &capsulev1beta2.Tenant{
				ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-selector-" + suffix + "-", Labels: map[string]string{"env": "e2e"}},
				Spec: capsulev1beta2.TenantSpec{Owners: rbac.OwnerListSpec{{CoreOwnerSpec: rbac.CoreOwnerSpec{
					UserSpec: rbac.UserSpec{Kind: "User", Name: "e2e-selector-owner-" + suffix},
				}}}},
			}
			Expect(k8sClient.Create(ctx, tnt)).To(Succeed())
			DeferCleanup(func() { EventuallyDeletion(tnt) })
			TenantReady(tnt, metav1.ConditionTrue, defaultTimeoutInterval)
			return tnt
		}
		tenantA, tenantB = newTenant("a"), newTenant("b")
		ns = NewNamespace("", map[string]string{meta.TenantLabel: tenantA.Name})
		NamespaceCreation(ns, tenantA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
		DeferCleanup(func() { EventuallyDeletion(ns) })
		TenantNamespaceReady(tenantA, ns, 1)
		owner = impersonationClient(tenantA.Spec.Owners[0].Name, withDefaultGroups(nil))

		sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "restorer", Namespace: ns.Name}}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())
		Expect(k8sClient.Create(ctx, &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "restorer", Namespace: ns.Name},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "edit"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: ns.Name, Name: sa.Name}},
		})).To(Succeed())
		identity := "system:serviceaccount:" + ns.Name + ":" + sa.Name
		restorer = impersonationClient(identity, []string{"system:serviceaccounts", "system:serviceaccounts:" + ns.Name, "system:authenticated"})

		original := &capsulev1beta2.CapsuleConfiguration{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: defaultConfigurationName}, original)).To(Succeed())
		DeferCleanup(func() {
			ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
				configuration.Spec.Admission = *original.Spec.Admission.DeepCopy()
			})
		})
		condition := admissionregistrationv1.MatchCondition{
			Name:       "e2e-exclude-restorer-create",
			Expression: fmt.Sprintf("request.operation != 'CREATE' || request.userInfo.username != %q", identity),
		}
		ModifyCapsuleConfigurationOpts(func(configuration *capsulev1beta2.CapsuleConfiguration) {
			for _, hook := range configuration.Spec.Admission.Mutating.Webhooks {
				if hook.Name == "pvc.mutating.projectcapsule.dev" {
					hook.MatchConditions = append(hook.MatchConditions, condition)
				}
			}
			for _, hook := range configuration.Spec.Admission.Validating.Webhooks {
				if hook.Name == "pvc.validating.projectcapsule.dev" {
					hook.MatchConditions = append(hook.MatchConditions, condition)
				}
			}
		})
		Eventually(func(g Gomega) {
			mutating := &admissionregistrationv1.MutatingWebhookConfiguration{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(original.Spec.Admission.Mutating.Name)}, mutating)).To(Succeed())
			g.Expect(mutating.Webhooks).To(ContainElement(SatisfyAll(
				HaveField("Name", "pvc.mutating.projectcapsule.dev"), HaveField("MatchConditions", ContainElement(condition)),
			)))
			validating := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: string(original.Spec.Admission.Validating.Name)}, validating)).To(Succeed())
			g.Expect(validating.Webhooks).To(ContainElement(SatisfyAll(
				HaveField("Name", "pvc.validating.projectcapsule.dev"), HaveField("MatchConditions", ContainElement(condition)),
			)))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	newClaim := func(name string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name, Labels: map[string]string{"env": "e2e"}},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				StorageClassName: ptr.To(""),
				Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
				Selector:         &metav1.LabelSelector{MatchLabels: map[string]string{"velero.io/dynamic-pv-restore": ns.Name + "." + name}},
			},
		}
	}

	createRestoredClaim := func(name string) *corev1.PersistentVolumeClaim {
		pvc := newClaim(name)
		Eventually(func(g Gomega) {
			probe := pvc.DeepCopy()
			g.Expect(restorer.Create(context.Background(), probe, client.DryRunAll)).To(Succeed())
			g.Expect(probe.Spec.Selector).To(Equal(pvc.Spec.Selector))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Expect(restorer.Create(context.Background(), pvc)).To(Succeed())
		DeferCleanup(func() { EventuallyDeletion(pvc) })
		return pvc
	}

	createVolume := func(pvc *corev1.PersistentVolumeClaim, tenant string, restoreID string) *corev1.PersistentVolume {
		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "e2e-selector-pv-", Labels: map[string]string{
				"env": "e2e", meta.TenantLabel: tenant, "velero.io/dynamic-pv-restore": restoreID,
			}},
			Spec: corev1.PersistentVolumeSpec{
				AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				PersistentVolumeSource:        corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/capsule-e2e-selector"}},
			},
		}
		Expect(k8sClient.Create(context.Background(), pv)).To(Succeed())
		DeferCleanup(func() {
			// Release the claim before deleting its bound Retain volume.
			EventuallyDeletion(pvc)
			EventuallyDeletion(pv)
		})
		return pv
	}

	It("allows Pending claim updates and controller binding without changing a restored selector", func() {
		ctx := context.Background()
		pvc := createRestoredClaim("restored")
		selector := pvc.Spec.Selector.DeepCopy()
		Eventually(func() error {
			current := &corev1.PersistentVolumeClaim{}
			if err := owner.Get(ctx, client.ObjectKeyFromObject(pvc), current); err != nil {
				return err
			}
			current.Annotations = map[string]string{"restore-update": "accepted"}
			return owner.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &corev1.PersistentVolumeClaim{}
			g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(pvc), current)).To(Succeed())
			g.Expect(current.Status.Phase).To(Equal(corev1.ClaimPending))
			g.Expect(current.Spec.Selector).To(Equal(selector))
			g.Expect(current.Annotations).To(HaveKeyWithValue("restore-update", "accepted"))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		pv := createVolume(pvc, tenantA.Name, selector.MatchLabels["velero.io/dynamic-pv-restore"])
		Eventually(func(g Gomega) {
			current := &corev1.PersistentVolumeClaim{}
			g.Expect(owner.Get(ctx, client.ObjectKeyFromObject(pvc), current)).To(Succeed())
			g.Expect(current.Status.Phase).To(Equal(corev1.ClaimBound))
			g.Expect(current.Spec.VolumeName).To(Equal(pv.Name))
			g.Expect(current.Spec.Selector).To(Equal(selector))
			volume := &corev1.PersistentVolume{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pv), volume)).To(Succeed())
			g.Expect(volume.Spec.ClaimRef).NotTo(BeNil())
			g.Expect(volume.Spec.ClaimRef.UID).To(Equal(current.UID))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
	})

	It("rejects selector tampering on Pending claims and preserves CREATE enforcement", func() {
		ctx := context.Background()
		for _, actor := range []client.Client{owner, restorer} {
			var pvc *corev1.PersistentVolumeClaim
			if actor == restorer {
				pvc = createRestoredClaim("restored")
			} else {
				pvc = newClaim("ordinary")
				Expect(owner.Create(ctx, pvc)).To(Succeed())
				DeferCleanup(func() { EventuallyDeletion(pvc) })
			}
			if actor == owner {
				Expect(pvc.Spec.Selector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{
					Key: meta.TenantLabel, Operator: metav1.LabelSelectorOpIn, Values: []string{tenantA.Name},
				}))
			}
			selector := pvc.Spec.Selector.DeepCopy()
			Eventually(func() error {
				current := &corev1.PersistentVolumeClaim{}
				if err := owner.Get(ctx, client.ObjectKeyFromObject(pvc), current); err != nil {
					return err
				}
				current.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{meta.TenantLabel: tenantB.Name}}
				err := owner.Update(ctx, current)
				if apierrors.IsConflict(err) {
					return err
				}
				Expect(err).To(HaveOccurred(), "selector tampering must never succeed")
				Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected API immutability rejection, got %v", err)
				Expect(err.Error()).To(ContainSubstring("spec is immutable"))
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			current := &corev1.PersistentVolumeClaim{}
			Expect(owner.Get(ctx, client.ObjectKeyFromObject(pvc), current)).To(Succeed())
			Expect(current.Spec.Selector).To(Equal(selector))
			Expect(current.Spec.VolumeName).To(BeEmpty())
		}
	})

	It("scopes additional restore access to its audience and still denies cross-tenant binding", func() {
		ctx := context.Background()
		pvc := createRestoredClaim("restored")
		grant := &rules.NamespaceRuleBodyTenant{NamespaceRuleBodyNamespace: &rules.NamespaceRuleBodyNamespace{
			Audience: []rules.Audience{{Kind: rules.AudienceKindGroup, Name: "system:serviceaccounts:" + ns.Name}},
			Enforce: &rules.NamespaceRuleEnforceBody{
				Action:     rules.ActionTypeAllow,
				Conditions: []rules.AdmissionCondition{{Expression: "request.operation == 'UPDATE'"}},
				Storage:    rules.NamespaceRuleEnforceStorageBody{Volumes: []rules.PersistentVolumeMatch{{Selector: &metav1.LabelSelector{}}}},
			},
		}}
		Eventually(func() error {
			current := &capsulev1beta2.Tenant{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(tenantA), current); err != nil {
				return err
			}
			current.Spec.Rules = []*rules.NamespaceRuleBodyTenant{grant.DeepCopy()}
			return k8sClient.Update(ctx, current)
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			status := &capsulev1beta2.RuleStatus{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Namespace: ns.Name, Name: meta.NameForManagedRuleStatus()}, status)).To(Succeed())
			g.Expect(status.Status.ObservedGeneration).To(Equal(status.Generation))
			ready := status.Status.Conditions.GetConditionByType(meta.ReadyCondition)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			g.Expect(status.Status.Rules).To(HaveLen(1))
			g.Expect(status.Status.Rules[0].Audience).To(Equal(grant.Audience))
			g.Expect(status.Status.Rules[0].Enforce).To(Equal(grant.Enforce))
		}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())

		for _, tenant := range []string{tenantB.Name, ""} {
			pv := createVolume(pvc, tenant, "does-not-match")
			if tenant == "" {
				Eventually(func() error {
					current := &corev1.PersistentVolume{}
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(pv), current); err != nil {
						return err
					}
					delete(current.Labels, meta.TenantLabel)
					return k8sClient.Update(ctx, current)
				}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			}
			Eventually(func() error {
				current := &corev1.PersistentVolumeClaim{}
				if err := owner.Get(ctx, client.ObjectKeyFromObject(pvc), current); err != nil {
					return err
				}
				current.Spec.VolumeName = pv.Name
				err := owner.Update(ctx, current)
				if apierrors.IsConflict(err) {
					return err
				}
				Expect(err).To(HaveOccurred(), "unauthorized PV binding must never succeed")
				Expect(err.Error()).To(ContainSubstring("pvc.validating.projectcapsule.dev"))
				if tenant == "" {
					Expect(err.Error()).To(ContainSubstring("missing the Tenant label"))
				} else {
					Expect(err.Error()).To(ContainSubstring("cross-tenant"))
				}
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			current := &corev1.PersistentVolumeClaim{}
			Expect(owner.Get(ctx, client.ObjectKeyFromObject(pvc), current)).To(Succeed())
			Expect(current.Spec.VolumeName).To(BeEmpty())
			Expect(current.Spec.Selector).To(Equal(pvc.Spec.Selector))

			Eventually(func() error {
				current := &corev1.PersistentVolumeClaim{}
				if err := restorer.Get(ctx, client.ObjectKeyFromObject(pvc), current); err != nil {
					return err
				}
				base := current.DeepCopy()
				current.Spec.VolumeName = pv.Name
				err := restorer.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
				if apierrors.IsConflict(err) || tenant == "" {
					return err
				}
				Expect(err).To(HaveOccurred(), "a restore grant must not bypass PV ownership")
				Expect(err.Error()).To(ContainSubstring("cross-tenant"))
				return nil
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
			Eventually(func(g Gomega) {
				current := &corev1.PersistentVolumeClaim{}
				g.Expect(restorer.Get(ctx, client.ObjectKeyFromObject(pvc), current)).To(Succeed())
				g.Expect(current.Spec.Selector).To(Equal(pvc.Spec.Selector))
				if tenant == "" {
					g.Expect(current.Spec.VolumeName).To(Equal(pv.Name))
				} else {
					g.Expect(current.Spec.VolumeName).To(BeEmpty())
				}
			}, defaultTimeoutInterval, defaultPollInterval).Should(Succeed())
		}
	})
})
